package telemetry

import (
	"errors"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/sirkostya009/barm"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// query runs ev through the hook the way barm does, and returns its span
func query(t *testing.T, ev barm.QueryEvent) sdktrace.ReadOnlySpan {
	t.Helper()
	rec, _, tp := recorder()
	h := QueryHook(tp)
	ctx := h.BeforeQuery(t.Context(), &ev)
	if !ev.StartedAt.IsZero() {
		ev.Duration = 3 * time.Millisecond
		// as barm reports a call that answered only at the end
		if ev.FirstResponse == 0 {
			ev.FirstResponse = ev.Duration
		}
	}
	h.AfterQuery(ctx, &ev)
	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("%d spans", len(spans))
	}
	return spans[0]
}

func spanAttrs(s sdktrace.ReadOnlySpan) map[attribute.Key]attribute.Value {
	m := make(map[attribute.Key]attribute.Value, len(s.Attributes()))
	for _, kv := range s.Attributes() {
		m[kv.Key] = kv.Value
	}
	return m
}

var at = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

// a builder knows its table: the span says what it did to which one. The
// schema stays out, a tenant's is one of unboundedly many.
func TestQueryHook_Span_NamedAfterTable(t *testing.T) {
	t.Parallel()
	s := query(t, barm.QueryEvent{Op: "SELECT", Table: "users", Schema: "tenant_06ge", StartedAt: at, Rows: 2, FirstResponse: time.Millisecond})

	if s.Name() != "SELECT users" {
		t.Errorf("name %q", s.Name())
	}
	a := spanAttrs(s)
	if a["db.collection.name"].AsString() != "users" || a["db.query.summary"].AsString() != "SELECT users" || a["db.response.returned_rows"].AsInt64() != 2 {
		t.Errorf("attributes %v", s.Attributes())
	}
	if !s.StartTime().Equal(at) || !s.EndTime().Equal(at.Add(3*time.Millisecond)) {
		t.Errorf("times %s .. %s", s.StartTime(), s.EndTime())
	}
	if ev := s.Events(); len(ev) != 1 || ev[0].Name != "first response" || !ev[0].Time.Equal(at.Add(time.Millisecond)) {
		t.Errorf("events %v", ev)
	}
}

func TestQueryHook_Span_NamedAfterPrepared(t *testing.T) {
	t.Parallel()
	s := query(t, barm.QueryEvent{Op: "SELECT", Table: "users", Prepared: "users.byEmail", StartedAt: at})
	if s.Name() != "users.byEmail" || spanAttrs(s)["db.query.summary"].AsString() != "users.byEmail" {
		t.Errorf("span %q %v", s.Name(), s.Attributes())
	}
}

// raw sql has no table: the operation alone names it, and nothing claims a summary
func TestQueryHook_Span_Raw(t *testing.T) {
	t.Parallel()
	s := query(t, barm.QueryEvent{Op: "SELECT", StartedAt: at})
	a := spanAttrs(s)
	_, collection := a["db.collection.name"]
	_, summary := a["db.query.summary"]
	if s.Name() != "SELECT" || collection || summary {
		t.Errorf("span %q %v", s.Name(), s.Attributes())
	}
}

// an exec reads no rows: it has no returned_rows, rather than a false zero
func TestQueryHook_Span_Exec_NoReturnedRows(t *testing.T) {
	t.Parallel()
	s := query(t, barm.QueryEvent{Op: "DELETE", Table: "users", StartedAt: at, Result: execResult{}})
	if _, ok := spanAttrs(s)["db.response.returned_rows"]; ok {
		t.Errorf("attributes %v", s.Attributes())
	}
}

type execResult struct{}

func (execResult) LastInsertId() (int64, error) { return 0, nil }
func (execResult) RowsAffected() (int64, error) { return 1, nil }

// a query that never reached the database has no times of its own: its span
// is now, not the zero time
func TestQueryHook_Span_Unsent(t *testing.T) {
	t.Parallel()
	before := time.Now()
	s := query(t, barm.QueryEvent{Op: "SELECT", Table: "users", Err: errors.New("no connection")})
	if s.StartTime().Before(before) || s.EndTime().Before(s.StartTime()) || len(s.Events()) != 1 {
		t.Errorf("times %s .. %s, events %v", s.StartTime(), s.EndTime(), s.Events())
	}
	if _, ok := spanAttrs(s)["db.response.returned_rows"]; ok {
		t.Error("an unsent query reports rows")
	}
}

type sqlStateError struct{ state string }

func (e sqlStateError) Error() string    { return "pg: " + e.state }
func (e sqlStateError) SQLState() string { return e.state }

// a database error says what went wrong in its SQLSTATE, wrapped or not
func TestQueryHook_Span_Failure_CarriesSQLState(t *testing.T) {
	t.Parallel()
	s := query(t, barm.QueryEvent{Op: "INSERT", Table: "users", StartedAt: at, Err: fmt.Errorf("insert: %w", sqlStateError{"23505"})})
	a := spanAttrs(s)
	if a["db.response.status_code"].AsString() != "23505" || a["error.type"].AsString() != "23505" {
		t.Errorf("attributes %v", s.Attributes())
	}

	s = query(t, barm.QueryEvent{Op: "SELECT", StartedAt: at, Err: errors.New("conn closed")})
	if _, ok := spanAttrs(s)["db.response.status_code"]; ok {
		t.Error("a status code on an error without one")
	}
}

// RETURNING reads rows back, so an exec with it counts them like a select
func TestQueryHook_Span_ExecReturning_ReturnedRows(t *testing.T) {
	t.Parallel()
	s := query(t, barm.QueryEvent{Op: "INSERT", Table: "users", StartedAt: at, Rows: 3})
	if v, ok := spanAttrs(s)["db.response.returned_rows"]; !ok || v.AsInt64() != 3 {
		t.Errorf("returned rows %v %t", v, ok)
	}
}

// a failed exec has no Result either, which must not read as zero rows returned
func TestQueryHook_Span_FailedExec_NoReturnedRows(t *testing.T) {
	t.Parallel()
	s := query(t, barm.QueryEvent{Op: "DELETE", Table: "users", StartedAt: at, Err: sqlStateError{"23503"}})
	if _, ok := spanAttrs(s)["db.response.returned_rows"]; ok {
		t.Errorf("attributes %v", s.Attributes())
	}
}

func TestQueryHook_Span_CallerBatchRelationStats(t *testing.T) {
	t.Parallel()
	caller := &runtime.Frame{Function: "goserver/internal/repository.(*userRepository).Get", File: "/src/users.go", Line: 57}
	ev := barm.QueryEvent{Op: "SELECT", Table: "posts", Relation: "User.Posts", Caller: caller, BatchSize: 3, BatchIndex: 1, StartedAt: at}
	ev.Wait, ev.Described, ev.PID = time.Millisecond, true, 4242
	s := query(t, ev)

	a := spanAttrs(s)
	for key, want := range map[attribute.Key]any{
		"code.function.name":      caller.Function,
		"code.file.path":          caller.File,
		"code.line.number":        int64(57),
		"db.operation.batch.size": int64(3),
		"barm.relation":           "User.Posts",
		"barm.described":          true,
		"barm.backend_pid":        int64(4242),
	} {
		if got := a[key].AsInterface(); got != want {
			t.Errorf("%s = %v, want %v", key, got, want)
		}
	}
	if ev := s.Events(); len(ev) == 0 || ev[0].Name != "connection acquired" || !ev[0].Time.Equal(at.Add(time.Millisecond)) {
		t.Errorf("events %v", ev)
	}
}

// one query is no batch, and a driver that reported nothing adds nothing
func TestQueryHook_Span_NoBatchNoStats(t *testing.T) {
	t.Parallel()
	s := query(t, barm.QueryEvent{Op: "SELECT", Table: "users", BatchSize: 1, StartedAt: at})
	a := spanAttrs(s)
	for _, key := range []attribute.Key{"db.operation.batch.size", "code.function.name", "barm.described", "barm.backend_pid", "barm.relation"} {
		if _, ok := a[key]; ok {
			t.Errorf("%s set", key)
		}
	}
	if len(s.Events()) != 0 {
		t.Errorf("events %v", s.Events())
	}
}
