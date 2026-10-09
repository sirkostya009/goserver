package telemetry

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/getsentry/sentry-go"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"goserver/domain"
)

// sentryOn is set once by Setup, before anything serves.
var sentryOn bool

type requestKey struct{}

// WithRequest keeps r in ctx for the issues reported under it. The Sentry scope
// is only built when one is, so a request that fails nothing pays one context
// value, and nothing at all with Sentry off.
func WithRequest(ctx context.Context, r *http.Request) context.Context {
	if !sentryOn {
		return ctx
	}
	return context.WithValue(ctx, requestKey{}, r)
}

// FailSpan marks the span in ctx failed with err. It is for the request's
// outcome: an error handled mid-stack leaves its span alone.
func FailSpan(ctx context.Context, err error) {
	span := trace.SpanFromContext(ctx)
	span.RecordError(err)
	span.SetStatus(codes.Error, "")
}

// ReportPanic fails the span in ctx and opens a Sentry issue for rec. Call it
// from the deferred func that recovered, so the stack still reaches the panic.
func ReportPanic(ctx context.Context, rec any) {
	trace.SpanFromContext(ctx).SetStatus(codes.Error, "panic")
	if hub := hubFor(ctx); hub != nil {
		hub.RecoverWithContext(ctx, rec)
	}
}

// issueHandler opens a Sentry issue for every Error record that carries an
// error, however deep in the stack it was logged. The stack is the error's own
// when it has one (see errs), otherwise the logging call's.
type issueHandler struct{ slog.Handler }

// Enabled takes every Error, whatever level the wrapped handler logs at.
func (h issueHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= slog.LevelError || h.Handler.Enabled(ctx, level)
}

func (h issueHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= slog.LevelError {
		if err := recordError(r); err != nil {
			if hub := hubFor(ctx); hub != nil {
				hub.CaptureException(err)
			}
		}
	}
	if !h.Handler.Enabled(ctx, r.Level) {
		return nil
	}
	return h.Handler.Handle(ctx, r)
}

func (h issueHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return issueHandler{h.Handler.WithAttrs(attrs)}
}

func (h issueHandler) WithGroup(name string) slog.Handler {
	return issueHandler{h.Handler.WithGroup(name)}
}

func recordError(r slog.Record) (err error) {
	r.Attrs(func(a slog.Attr) bool {
		err, _ = a.Value.Resolve().Any().(error)
		return err == nil
	})
	return err
}

// hubFor clones ctx's hub, or the global one, so the scope it fills for this
// issue never leaks into another. It returns nil when Sentry is off. The trace
// is the innermost span's, so an issue leads to the exact span it happened in.
func hubFor(ctx context.Context) *sentry.Hub {
	current := sentry.GetHubFromContext(ctx)
	if current == nil {
		current = sentry.CurrentHub()
	}
	if current.Client() == nil {
		return nil
	}
	hub := current.Clone()
	scope := hub.Scope()
	if r, ok := ctx.Value(requestKey{}).(*http.Request); ok {
		scope.SetRequest(r)
	}
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		scope.SetPropagationContext(sentry.PropagationContext{
			TraceID: sentry.TraceID(sc.TraceID()),
			SpanID:  sentry.SpanID(sc.SpanID()),
		})
	}
	if tc, ok := domain.TenantFrom(ctx); ok {
		scope.SetTag("tenant", tc.ID.String())
	}
	return hub
}
