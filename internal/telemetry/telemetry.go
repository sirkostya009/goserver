// Package telemetry wires OpenTelemetry for traces, metrics and logs, and
// Sentry for issues only: Sentry never sees a span, and a log line only when it
// is an Error carrying an error, which becomes an issue.
//
// Both are configured by their own standard env vars, OTEL_* and SENTRY_*. A
// signal with no exporter configured stays off, so a dev run exports nothing and
// dials nothing.
package telemetry

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"slices"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/sirkostya009/barm"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/contrib/exporters/autoexport"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

const scope = "goserver"

// Telemetry is what Setup installed, to flush on the way out.
type Telemetry struct {
	shutdowns []func(context.Context) error
	tracing   bool
}

// Setup installs the global otel providers for every configured signal, bridges
// slog into otel logs, and starts Sentry when SENTRY_DSN is set, turning logged
// errors into issues. Call it after logger.Setup: it wraps the default logger
// rather than replacing it.
func Setup(ctx context.Context) (*Telemetry, error) {
	t := &Telemetry{}
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	res, err := resource.New(ctx,
		resource.WithAttributes(semconv.ServiceName(scope)),
		resource.WithFromEnv(), // OTEL_SERVICE_NAME and OTEL_RESOURCE_ATTRIBUTES win over the above
		resource.WithTelemetrySDK(),
		resource.WithHost(),
	)
	if err != nil {
		return nil, err
	}

	if configured("TRACES") {
		exp, err := autoexport.NewSpanExporter(ctx)
		if err != nil {
			return nil, t.fail(ctx, err)
		}
		tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exp), sdktrace.WithResource(res))
		otel.SetTracerProvider(tp)
		t.shutdowns = append(t.shutdowns, tp.Shutdown)
		t.tracing = true
	}

	if configured("METRICS") {
		reader, err := autoexport.NewMetricReader(ctx)
		if err != nil {
			return nil, t.fail(ctx, err)
		}
		mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader), sdkmetric.WithResource(res))
		otel.SetMeterProvider(mp)
		t.shutdowns = append(t.shutdowns, mp.Shutdown)
		if err := runtime.Start(runtime.WithMeterProvider(mp)); err != nil {
			return nil, t.fail(ctx, err)
		}
	}

	if configured("LOGS") {
		exp, err := autoexport.NewLogExporter(ctx)
		if err != nil {
			return nil, t.fail(ctx, err)
		}
		lp := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewBatchProcessor(exp)), sdklog.WithResource(res))
		otel.SetLoggerProvider(lp)
		t.shutdowns = append(t.shutdowns, lp.Shutdown)
		otelHandler := otelslog.NewHandler(scope, otelslog.WithLoggerProvider(lp))
		slog.SetDefault(slog.New(slog.NewMultiHandler(slog.Default().Handler(), otelHandler)))
	}

	// DSN, environment and release all come from SENTRY_* or the build info
	if os.Getenv("SENTRY_DSN") != "" {
		if err := sentry.Init(sentry.ClientOptions{AttachStacktrace: true}); err != nil {
			return nil, t.fail(ctx, err)
		}
		sentryOn = true
		slog.SetDefault(slog.New(issueHandler{slog.Default().Handler()}))
		t.shutdowns = append(t.shutdowns, func(ctx context.Context) error {
			timeout := 5 * time.Second
			if d, ok := ctx.Deadline(); ok {
				timeout = time.Until(d)
			}
			if !sentry.Flush(timeout) {
				return errors.New("sentry: flush timed out")
			}
			return nil
		})
	}

	return t, nil
}

// configured reports whether signal (TRACES, METRICS or LOGS) has an exporter:
// named outright, or implied by an otlp endpoint. autoexport alone would fall
// back to otlp on localhost.
func configured(signal string) bool {
	return os.Getenv("OTEL_"+signal+"_EXPORTER") != "" ||
		os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" ||
		os.Getenv("OTEL_EXPORTER_OTLP_"+signal+"_ENDPOINT") != ""
}

// Shutdown flushes and stops everything Setup started, newest first.
func (t *Telemetry) Shutdown(ctx context.Context) error {
	var errs []error
	for _, shutdown := range slices.Backward(t.shutdowns) {
		errs = append(errs, shutdown(ctx))
	}
	return errors.Join(errs...)
}

// BarmOptions traces queries when tracing is on, with where each was called from
// and what the driver saw. With it off it returns none, so barm keeps its
// hookless fast path.
func (t *Telemetry) BarmOptions() []barm.Option {
	if !t.tracing {
		return nil
	}
	return []barm.Option{barm.WithHook(QueryHook(otel.GetTracerProvider())), barm.WithCaller(), barm.WithCallStats()}
}

func (t *Telemetry) fail(ctx context.Context, err error) error {
	return errors.Join(err, t.Shutdown(ctx))
}
