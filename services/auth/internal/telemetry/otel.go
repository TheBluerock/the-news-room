package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Init configures a global TracerProvider (OTLP→Tempo) and MeterProvider (Prometheus).
// Returns a shutdown function and an http.Handler for the /metrics endpoint.
func Init(ctx context.Context, serviceName string) (shutdown func(context.Context) error, metricsHandler http.Handler, err error) {
	res, err := resource.New(ctx,
		resource.WithAttributes(semconv.ServiceName(serviceName)),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("otel resource: %w", err)
	}

	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		endpoint = "tempo:4317"
	}
	traceExp, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("otel trace exporter: %w", err)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	promExp, err := prometheus.New()
	if err != nil {
		return nil, nil, fmt.Errorf("otel prometheus exporter: %w", err)
	}
	mp := metric.NewMeterProvider(
		metric.WithReader(promExp),
		metric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	shutdown = func(ctx context.Context) error {
		_ = tp.Shutdown(ctx)
		_ = mp.Shutdown(ctx)
		return nil
	}
	return shutdown, promhttp.Handler(), nil
}
