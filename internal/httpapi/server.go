package httpapi

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"goserver/domain"
	"goserver/internal/logger"
	"goserver/internal/service"
	"goserver/internal/telemetry"
)

// New wires the http layer over the services below it. An empty adminToken
// leaves the operator's routes closed to everyone.
func New(db Pinger, services service.Container, adminToken string) *Server {
	s := &Server{
		DB:      db,
		Tenants: services.Tenants,
		Users:   services.Users,
		Posts:   services.Posts,
		Auth:    services.Auth,
		Keys:    services.APIKeys,
		obs:     newInstruments(otel.GetTracerProvider(), otel.GetMeterProvider(), otel.GetTextMapPropagator()),
	}
	if adminToken != "" {
		h := sha256.Sum256([]byte(adminToken))
		s.adminHash = h[:]
	}
	return s
}

// Server wraps the generated router with what rr guards cannot do, since they
// can neither replace the request nor wrap the writer: it refuses cross-origin
// writes, resolves the caller's credential into a tenant and principal in the
// request context, traces, recovers panics, and logs a line with the final
// status.
type Server struct {
	Api

	obs instruments
	// adminHash is the sha-256 of the operator's token, nil when there is none
	adminHash []byte
}

var crossOrigin = http.NewCrossOriginProtection()

type instruments struct {
	tracer     trace.Tracer
	duration   metric.Float64Histogram
	propagator propagation.TextMapPropagator
}

func newInstruments(tp trace.TracerProvider, mp metric.MeterProvider, p propagation.TextMapPropagator) instruments {
	duration, err := mp.Meter("goserver/internal/httpapi").Float64Histogram(
		"http.server.request.duration",
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10),
	)
	if err != nil {
		panic(err) // only for an invalid name or unit, which are constants
	}
	return instruments{tracer: tp.Tracer("goserver/internal/httpapi"), duration: duration, propagator: p}
}

type statusWriter struct {
	http.ResponseWriter

	status int
}

func (sw *statusWriter) WriteHeader(code int) {
	if sw.status == 0 {
		sw.status = code
	}
	sw.ResponseWriter.WriteHeader(code)
}

func (sw *statusWriter) Write(b []byte) (int, error) {
	if sw.status == 0 {
		sw.status = http.StatusOK
	}
	return sw.ResponseWriter.Write(b)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	sw := statusWriter{ResponseWriter: w}

	ctx := s.obs.propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
	ctx, span := s.obs.tracer.Start(ctx, method(r.Method), trace.WithSpanKind(trace.SpanKindServer), trace.WithTimestamp(start))
	defer span.End()
	ctx = telemetry.WithRequest(ctx, r)

	// registered before authenticate, which can panic too. It reads ctx and r
	// once they are final.
	defer func() {
		if rec := recover(); rec != nil {
			// a string, never an error, which would be captured a second time
			slog.ErrorContext(ctx, "panic", slog.String("err", fmt.Sprint(rec)), slog.String("path", r.URL.Path))
			telemetry.ReportPanic(ctx, rec)
			if sw.status == 0 {
				http.Error(&sw, "internal error", http.StatusInternalServerError)
			}
		}
		if sw.status == 0 {
			sw.status = http.StatusOK
		}
		dur := time.Since(start)
		logger.Request(r, sw.status, dur)
		s.obs.finish(ctx, span, r, sw.status, dur)
	}()

	// both read headers only, so they run before the context is final and the
	// request is rebuilt once
	refused := crossOrigin.Check(r)
	var authErr error
	if refused == nil {
		var authed context.Context
		if authed, authErr = s.authenticate(ctx, r); authErr == nil {
			ctx = authed
		}
	}
	// the router stamps Pattern on the request it is handed, so the one the log
	// reads has to be the one that goes in
	r = r.WithContext(ctx)

	switch {
	case refused != nil:
		http.Error(&sw, "cross-origin request refused", http.StatusForbidden)
	case authErr != nil:
		handleError(ctx, &sw, authErr)
	default:
		s.Api.ServeHTTP(&sw, r)
	}
}

// finish names the span after the matched route, never the raw path, which
// would make one span name per id.
func (o instruments) finish(ctx context.Context, span trace.Span, r *http.Request, status int, dur time.Duration) {
	route := r.Pattern
	if i := strings.IndexByte(route, ' '); i >= 0 {
		route = route[i+1:]
	}

	attrs := make([]attribute.KeyValue, 0, 4)
	attrs = append(attrs, semconv.HTTPRequestMethodKey.String(method(r.Method)), semconv.HTTPResponseStatusCode(status))
	if route != "" {
		attrs = append(attrs, semconv.HTTPRoute(route))
	}
	if status >= http.StatusInternalServerError {
		attrs = append(attrs, semconv.ErrorTypeKey.String(strconv.Itoa(status)))
	}
	o.duration.Record(ctx, dur.Seconds(), metric.WithAttributes(attrs...))

	if !span.IsRecording() {
		return
	}
	if route != "" {
		span.SetName(r.Method + " " + route)
	}
	attrs = append(attrs, semconv.URLPath(r.URL.Path))
	if tc, ok := domain.TenantFrom(ctx); ok {
		attrs = append(attrs, attribute.String("tenant.id", tc.ID.String()))
	}
	span.SetAttributes(attrs...)
	if status >= http.StatusInternalServerError {
		span.SetStatus(codes.Error, "")
	}
}

// method is the method as a metric may label it: a client can send any string
// there, and each new one would be a new series.
func method(m string) string {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodOptions, http.MethodConnect, http.MethodTrace, "QUERY":
		return m
	}
	return "_OTHER"
}
