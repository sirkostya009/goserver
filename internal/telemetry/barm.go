package telemetry

import (
	"context"
	"database/sql"
	"errors"

	"github.com/sirkostya009/barm"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
)

// QueryHook makes each barm query a client span under the span ctx carries,
// named after what it does to which table, or after the name it was prepared
// under. The query text has placeholders only, arguments are never recorded,
// and the table is never schema-qualified: tenant schemas are unbounded.
func QueryHook(tp trace.TracerProvider) barm.QueryHook {
	tracer := tp.Tracer(scope)
	return barm.QueryHook{
		BeforeQuery: func(ctx context.Context, ev *barm.QueryEvent) context.Context {
			attrs := make([]attribute.KeyValue, 3, 10)
			attrs[0], attrs[1], attrs[2] = semconv.DBSystemNamePostgreSQL, semconv.DBOperationName(ev.Op), semconv.DBQueryText(ev.Query)
			name := ev.Op
			if ev.Table != "" {
				name += " " + ev.Table
				attrs = append(attrs, semconv.DBCollectionName(ev.Table))
			}
			if ev.Prepared != "" {
				name = ev.Prepared
			}
			if name != ev.Op {
				attrs = append(attrs, semconv.DBQuerySummary(name))
			}
			if ev.Relation != "" {
				attrs = append(attrs, attribute.String("barm.relation", ev.Relation))
			}
			if ev.BatchSize > 1 {
				attrs = append(attrs, semconv.DBOperationBatchSize(int(ev.BatchSize)))
			}
			if c := ev.Caller; c != nil {
				attrs = append(attrs, semconv.CodeFunctionName(c.Function), semconv.CodeFilePath(c.File), semconv.CodeLineNumber(c.Line))
			}

			opts := []trace.SpanStartOption{trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(attrs...)}
			// zero for a query that never reached the database
			if !ev.StartedAt.IsZero() {
				opts = append(opts, trace.WithTimestamp(ev.StartedAt))
			}
			//nolint:spancheck // AfterQuery ends it, from the ctx this returns
			ctx, _ = tracer.Start(ctx, name, opts...)
			return ctx
		},
		AfterQuery: func(ctx context.Context, ev *barm.QueryEvent) {
			span := trace.SpanFromContext(ctx)
			if ev.Err != nil && !errors.Is(ev.Err, sql.ErrNoRows) {
				span.RecordError(ev.Err)
				span.SetStatus(codes.Error, "")
				type sqlStater interface {
					error
					SQLState() string
				}
				if s, ok := errors.AsType[sqlStater](ev.Err); ok {
					state := s.SQLState()
					span.SetAttributes(semconv.DBResponseStatusCode(state), semconv.ErrorTypeKey.String(state))
				}
			}
			if ev.StartedAt.IsZero() {
				span.End()
				return
			}
			// barm counts Rows for whatever reads rows back, RETURNING included, and
			// hands a plain exec a Result instead. A failed exec has neither, and no
			// rows to claim.
			if ev.Err == nil && ev.Result == nil {
				span.SetAttributes(semconv.DBResponseReturnedRows(int(ev.Rows)))
			}
			if ev.Wait > 0 {
				span.AddEvent("connection acquired", trace.WithTimestamp(ev.StartedAt.Add(ev.Wait)))
			}
			if ev.Described {
				span.SetAttributes(attribute.Bool("barm.described", true))
			}
			if ev.PID != 0 {
				span.SetAttributes(attribute.Int64("barm.backend_pid", int64(ev.PID)))
			}
			if ev.FirstResponse < ev.Duration {
				span.AddEvent("first response", trace.WithTimestamp(ev.StartedAt.Add(ev.FirstResponse)))
			}
			span.End(trace.WithTimestamp(ev.StartedAt.Add(ev.Duration)))
		},
	}
}
