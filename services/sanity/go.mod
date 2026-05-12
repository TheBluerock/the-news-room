module github.com/newsroom/sanity

go 1.26.3

require (
	github.com/google/uuid v1.6.0
	github.com/prometheus/client_golang v1.23.2
	github.com/twmb/franz-go v1.20.7
	go.opentelemetry.io/otel v1.43.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.43.0
	go.opentelemetry.io/otel/exporters/prometheus v0.65.0
	go.opentelemetry.io/otel/sdk v1.43.0
	go.opentelemetry.io/otel/sdk/metric v1.43.0
	go.opentelemetry.io/otel/trace v1.43.0
)
