package main

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

// setupTelemetry sets up the global providers of OpenTelemetry, which
// oteltyr and otelpgx use, to export spans and metrics over OTLP/HTTP to
// OTEL_EXPORTER_OTLP_ENDPOINT, such as http://localhost:4318, the address
// of a collector. The other variables of the SDK configure the rest, such
// as OTEL_SERVICE_NAME, tasks by default. Without the endpoint, telemetry
// stays off: the global providers do nothing. The function it returns
// sends what's left and stops the providers.
func setupTelemetry(ctx context.Context) (shutdown func(context.Context) error, err error) {
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		return func(context.Context) error { return nil }, nil
	}
	res, err := resource.New(ctx,
		resource.WithAttributes(semconv.ServiceName("tasks")),
		resource.WithFromEnv(), // OTEL_SERVICE_NAME and OTEL_RESOURCE_ATTRIBUTES win
		resource.WithTelemetrySDK())
	if err != nil {
		return nil, err
	}
	spans, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}
	metrics, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(spans), sdktrace.WithResource(res))
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metrics)), sdkmetric.WithResource(res))
	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	// A request joins the trace of its caller by the header traceparent.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	return func(ctx context.Context) error {
		return errors.Join(tp.Shutdown(ctx), mp.Shutdown(ctx))
	}, nil
}

// newPool returns a pool of connections of config whose queries make
// spans and whose connections are measured, by otelpgx.
func newPool(ctx context.Context, config *pgxpool.Config) (*pgxpool.Pool, error) {
	config.ConnConfig.Tracer = otelpgx.NewTracer(otelpgx.WithSpanNameFunc(querySpanName))
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}
	if err := otelpgx.RecordStats(pool); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// querySpanName names the span of a query, and its db.operation.name,
// after the name that sqlc gives it, such as CreateProject, in the comment
// that starts its SQL, or else after its first word, such as BEGIN.
// otelpgx takes the first word, which for every query of sqlc is --.
func querySpanName(sql string) string {
	if rest, ok := strings.CutPrefix(sql, "-- name: "); ok {
		if name, _, ok := strings.Cut(rest, " "); ok {
			return name
		}
	}
	for word := range strings.FieldsSeq(sql) {
		return strings.ToUpper(word)
	}
	return "query"
}
