package telemetry

import (
	"context"
	"errors"
	"log/slog"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"uuid"

	"github.com/getsentry/sentry-go"
	"github.com/sirkostya009/barm"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"goserver/domain"
	"goserver/internal/errs"
	"goserver/internal/pgtest"
)

func TestMain(m *testing.M) {
	sentryOn = true // as Setup leaves it, so WithRequest keeps the request
	os.Exit(pgtest.Run(m))
}

func recorder() (*tracetest.SpanRecorder, trace.Tracer, trace.TracerProvider) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	return rec, tp.Tracer("test"), tp
}

func TestQueryHook_Span_ChildOfCaller(t *testing.T) {
	t.Parallel()
	rec, tracer, tp := recorder()
	db := barm.New(pgtest.DB(t).Pool(), barm.WithHook(QueryHook(tp)))

	ctx, parent := tracer.Start(t.Context(), "parent")
	if _, err := db.NewRaw("SELECT ?::int", 1).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	parent.End()

	spans := rec.Ended()
	if len(spans) != 2 {
		t.Fatalf("%d spans, want the query and its parent", len(spans))
	}
	q := spans[0]
	if q.Name() != "SELECT" || q.SpanKind() != trace.SpanKindClient {
		t.Errorf("span %q kind %v", q.Name(), q.SpanKind())
	}
	if q.Parent().SpanID() != parent.SpanContext().SpanID() {
		t.Error("query span is not a child of the caller's")
	}
	for _, kv := range q.Attributes() {
		if kv.Key == "db.query.text" && kv.Value.AsString() != "SELECT $1::int" {
			t.Errorf("query text %q", kv.Value.AsString())
		}
	}
	if q.Status().Code != codes.Unset {
		t.Errorf("status %v", q.Status())
	}
}

// the failing statement aborts the test's transaction, so this ends here
func TestQueryHook_Span_Failure_MarkedFailed(t *testing.T) {
	t.Parallel()
	rec, _, tp := recorder()
	db := barm.New(pgtest.DB(t).Pool(), barm.WithHook(QueryHook(tp)))

	if _, err := db.NewRaw("SELECT 1 FROM no_such_table").Exec(t.Context()); err == nil {
		t.Fatal("query on a missing table succeeded")
	}
	spans := rec.Ended()
	if len(spans) != 1 || spans[0].Status().Code != codes.Error || len(spans[0].Events()) != 1 {
		t.Fatalf("spans %v, want one failed with the error recorded", spans)
	}
	if got := spanAttrs(spans[0])["db.response.status_code"].AsString(); got != "42P01" {
		t.Errorf("status code %q, want undefined_table", got)
	}
}

// mockSentry puts a hub on ctx rather than initialising the global one, so
// these tests run in parallel
func mockSentry(t *testing.T, ctx context.Context) (context.Context, *sentry.MockTransport) {
	t.Helper()
	transport := &sentry.MockTransport{}
	client, err := sentry.NewClient(sentry.ClientOptions{Dsn: "https://key@sentry.invalid/1", Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	return sentry.SetHubOnContext(ctx, sentry.NewHub(client, sentry.NewScope())), transport
}

func origin() error { return errs.New("db on fire") }

func TestIssueHandler_LoggedError_IssueAtOrigin(t *testing.T) {
	t.Parallel()
	ctx, transport := mockSentry(t, t.Context())
	_, tracer, _ := recorder()
	tenant := domain.TenantID(uuid.MustParse("01a0c081-9683-7dff-a267-f090f4ce1d17"))
	r := httptest.NewRequestWithContext(ctx, "GET", "/api/users", nil)

	ctx, span := tracer.Start(WithRequest(domain.WithTenant(ctx, tenant), r), "deep")
	log := slog.New(issueHandler{slog.DiscardHandler})
	log.ErrorContext(ctx, "cleanup failed", slog.String("step", "x"), slog.Any("err", origin()))
	span.End()

	events := transport.Events()
	if len(events) != 1 {
		t.Fatalf("%d events, want 1", len(events))
	}
	ev := events[0]
	exc := ev.Exception[len(ev.Exception)-1]
	if exc.Value != "db on fire" || exc.Stacktrace == nil {
		t.Fatalf("exception %+v", exc)
	}
	// sentry lists frames outermost first
	if fn := exc.Stacktrace.Frames[len(exc.Stacktrace.Frames)-1].Function; fn != "origin" {
		t.Errorf("innermost frame %s, want where the error was made", fn)
	}
	if got := ev.Contexts["trace"]["span_id"]; got != sentry.SpanID(span.SpanContext().SpanID()) {
		t.Errorf("span id %v, want the span it was logged in", got)
	}
	if got := ev.Tags["tenant"]; got != tenant.String() {
		t.Errorf("tenant tag %q", got)
	}
	if ev.Request == nil || ev.Request.URL != "http://example.com/api/users" {
		t.Errorf("request %+v", ev.Request)
	}
}

func TestIssueHandler_NotAnIssue(t *testing.T) {
	t.Parallel()
	ctx, transport := mockSentry(t, t.Context())
	log := slog.New(issueHandler{slog.DiscardHandler})
	log.WarnContext(ctx, "tenant not migrated", slog.Any("err", errors.New("x")))
	log.ErrorContext(ctx, "no error attached", slog.String("err", "x"))

	if n := len(transport.Events()); n != 0 {
		t.Errorf("%d issues from records that are not errors", n)
	}
}

func TestIssueHandler_WithAttrs_StillReports(t *testing.T) {
	t.Parallel()
	ctx, transport := mockSentry(t, t.Context())
	log := slog.New(issueHandler{slog.DiscardHandler}).With("component", "x").WithGroup("g")
	log.ErrorContext(ctx, "failed", slog.Any("err", errors.New("x")))

	if n := len(transport.Events()); n != 1 {
		t.Errorf("%d issues, want 1", n)
	}
}

func TestReportPanic_Issue(t *testing.T) {
	t.Parallel()
	ctx, transport := mockSentry(t, t.Context())
	ReportPanic(ctx, "boom")

	events := transport.Events()
	if len(events) != 1 || events[0].Level != sentry.LevelFatal || events[0].Message != "boom" {
		t.Fatalf("events %+v", events)
	}
}

func TestReportPanic_SentryOff_NoOp(t *testing.T) {
	t.Parallel()
	ReportPanic(t.Context(), "x")
	slog.New(issueHandler{slog.DiscardHandler}).ErrorContext(t.Context(), "x", slog.Any("err", errors.New("x")))
}

// WithCaller names the code that ran the query, not barm
func TestQueryHook_Span_Caller(t *testing.T) {
	t.Parallel()
	rec, _, tp := recorder()
	db := barm.New(pgtest.DB(t).Pool(), barm.WithHook(QueryHook(tp)), barm.WithCaller())

	if _, err := db.NewRaw("SELECT 1").Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("%d spans", len(spans))
	}
	if fn := spanAttrs(spans[0])["code.function.name"].AsString(); !strings.HasSuffix(fn, ".TestQueryHook_Span_Caller") {
		t.Errorf("caller %q", fn)
	}
}

// a prepared statement with no arguments, prepared and run in one flush
func TestQueryHook_Span_Prepared(t *testing.T) {
	t.Parallel()
	rec, _, tp := recorder()
	db := barm.New(pgtest.DB(t).Pool(), barm.WithHook(QueryHook(tp)))

	for range 2 { // prepared, then by name
		if _, err := db.Select[domain.Tenant]().Prepare("tenants.all").Slice(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	spans := rec.Ended()
	if len(spans) != 2 || spans[0].Name() != "tenants.all" || spans[1].Name() != "tenants.all" {
		t.Fatalf("spans %v", spans)
	}
}
