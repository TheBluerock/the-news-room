module github.com/newsroom/auth

go 1.22

require (
	github.com/casbin/casbin/v2 v2.89.0
	github.com/casbin/pgx-adapter/v3 v3.2.0
	github.com/golang-jwt/jwt/v5 v5.2.1
	github.com/jackc/pgx/v5 v5.6.0
	github.com/redis/go-redis/v9 v9.5.3
	go.opentelemetry.io/otel v1.27.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.27.0
	go.opentelemetry.io/otel/sdk v1.27.0
	google.golang.org/grpc v1.64.0
)
