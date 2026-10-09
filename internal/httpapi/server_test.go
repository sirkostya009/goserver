package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"goserver/internal/auth"
)

func tracedHarness() (harness, *tracetest.SpanRecorder) {
	rec := tracetest.NewSpanRecorder()
	h := newHarness()
	h.obs = newInstruments(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)), metricnoop.NewMeterProvider(), propagation.TraceContext{})
	return h, rec
}

func onlySpan(t *testing.T, rec *tracetest.SpanRecorder) sdktrace.ReadOnlySpan {
	t.Helper()
	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("%d spans ended, want 1", len(spans))
	}
	return spans[0]
}

func attrs(s sdktrace.ReadOnlySpan) map[attribute.Key]attribute.Value {
	m := make(map[attribute.Key]attribute.Value, len(s.Attributes()))
	for _, kv := range s.Attributes() {
		m[kv.Key] = kv.Value
	}
	return m
}

func TestServer_Span_NamedAfterRoute(t *testing.T) {
	t.Parallel()
	h, rec := tracedHarness()
	h.do(t, request{method: "GET", path: "/api/users/" + guyText, tenant: acmeText})

	span := onlySpan(t, rec)
	if span.Name() != "GET /api/users/{id}" {
		t.Errorf("name %q", span.Name())
	}
	if span.SpanKind() != trace.SpanKindServer {
		t.Errorf("kind %v", span.SpanKind())
	}
	a := attrs(span)
	for key, want := range map[attribute.Key]string{
		"http.route":          "/api/users/{id}",
		"http.request.method": "GET",
		"url.path":            "/api/users/" + guyText,
		"tenant.id":           acmeText,
	} {
		if got := a[key].AsString(); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if got := a["http.response.status_code"].AsInt64(); got != http.StatusOK {
		t.Errorf("status code %d", got)
	}
	if span.Status().Code != codes.Unset {
		t.Errorf("status %v on a 200", span.Status())
	}
}

// an unmatched path must not become a span name, one per id would be unbounded
func TestServer_Span_Unmatched_NamedAfterMethod(t *testing.T) {
	t.Parallel()
	h, rec := tracedHarness()
	h.do(t, request{method: "GET", path: "/nope/123"})
	h.do(t, request{method: "BREW", path: "/nope/123"})

	spans := rec.Ended()
	if len(spans) != 2 || spans[0].Name() != "GET" || spans[1].Name() != "_OTHER" {
		t.Fatalf("names %q %q", spans[0].Name(), spans[1].Name())
	}
	if _, ok := attrs(spans[0])["http.route"]; ok {
		t.Error("http.route set with no route matched")
	}
}

func TestServer_Span_ContinuesIncomingTrace(t *testing.T) {
	t.Parallel()
	h, rec := tracedHarness()
	req := httptest.NewRequestWithContext(t.Context(), "GET", "/api/health", nil)
	req.Header.Set("Traceparent", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")
	h.ServeHTTP(httptest.NewRecorder(), req)

	span := onlySpan(t, rec)
	if got := span.SpanContext().TraceID().String(); got != "0af7651916cd43dd8448eb211c80319c" {
		t.Errorf("trace id %s", got)
	}
	if got := span.Parent().SpanID().String(); got != "b7ad6b7169203331" {
		t.Errorf("parent span %s", got)
	}
}

func TestServer_Span_ServerError_MarkedFailed(t *testing.T) {
	t.Parallel()
	h, rec := tracedHarness()
	h.users.err = errors.New("db on fire")
	h.do(t, request{method: "GET", path: "/api/users", tenant: acmeText})

	span := onlySpan(t, rec)
	if span.Status().Code != codes.Error {
		t.Errorf("status %v", span.Status())
	}
	if got := attrs(span)["error.type"].AsString(); got != "500" {
		t.Errorf("error.type %q", got)
	}
	events := span.Events()
	if len(events) != 1 || events[0].Name != "exception" {
		t.Fatalf("events %v, want the recorded error", events)
	}
}

// a 4xx is the client's fault: no error status, no issue
func TestServer_Span_ClientError_NotFailed(t *testing.T) {
	t.Parallel()
	h, rec := tracedHarness()
	h.do(t, request{method: "GET", path: "/api/users"})

	span := onlySpan(t, rec)
	if span.Status().Code != codes.Unset || len(span.Events()) != 0 {
		t.Errorf("status %v, events %v on a 401", span.Status(), span.Events())
	}
}

func TestServer_Span_Panic_MarkedFailed(t *testing.T) {
	t.Parallel()
	h, rec := tracedHarness()
	h.Users = panickingUsers{h.users}
	h.do(t, request{method: "GET", path: "/api/users", tenant: acmeText})

	if span := onlySpan(t, rec); span.Status().Code != codes.Error {
		t.Errorf("status %v", span.Status())
	}
}

// a client that hung up is no server failure: no 500, no failed span, no issue
func TestServer_ClientGone_NotFailed(t *testing.T) {
	t.Parallel()
	h, rec := tracedHarness()
	h.users.err = context.Canceled
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	req := httptest.NewRequestWithContext(ctx, "GET", "/api/users", nil)
	token, _ := auth.NewToken(auth.APIKey, acme)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != statusClientClosed {
		t.Errorf("status %d", w.Code)
	}
	if span := onlySpan(t, rec); span.Status().Code != codes.Unset {
		t.Errorf("status %v", span.Status())
	}
}

// the service's own cancellation, with the client still there, is a failure
func TestServer_CanceledInternally_Fails(t *testing.T) {
	t.Parallel()
	h, rec := tracedHarness()
	h.users.err = context.Canceled
	w := h.do(t, request{method: "GET", path: "/api/users", tenant: acmeText})

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status %d", w.Code)
	}
	if span := onlySpan(t, rec); span.Status().Code != codes.Error {
		t.Errorf("status %v", span.Status())
	}
}
