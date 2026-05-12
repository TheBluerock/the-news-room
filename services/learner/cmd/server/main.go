package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"

	learnerv1 "github.com/newsroom/proto/learner/v1"

	"github.com/newsroom/learner/internal/db"
	grpcserver "github.com/newsroom/learner/internal/grpc"
	"github.com/newsroom/learner/internal/health"
	"github.com/newsroom/learner/internal/restapi"
	"github.com/newsroom/learner/internal/telemetry"
	"github.com/newsroom/learner/internal/vault"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	var ready atomic.Bool

	telShutdown, metricsHandler, err := telemetry.Init(ctx, "learner-server")
	if err != nil {
		logger.Warn("telemetry init failed, continuing without observability", "err", err)
		metricsHandler = http.NotFoundHandler()
	}
	healthSrv := health.NewServer(&ready, metricsHandler)

	secrets, err := vault.Load("learner")
	if err != nil {
		logger.Error("vault load failed", "err", err)
		os.Exit(1)
	}
	postgresDSN, err := secrets.Require("postgres_dsn")
	if err != nil {
		logger.Error("missing postgres_dsn", "err", err)
		os.Exit(1)
	}
	redisAddr, err := secrets.Require("redis_addr")
	if err != nil {
		logger.Error("missing redis_addr", "err", err)
		os.Exit(1)
	}
	openAIKey, err := secrets.Require("openai_api_key")
	if err != nil {
		logger.Error("missing openai_api_key", "err", err)
		os.Exit(1)
	}

	pool, err := pgxpool.New(ctx, postgresDSN)
	if err != nil {
		logger.Error("postgres connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		logger.Error("postgres ping failed", "err", err)
		os.Exit(1)
	}
	if err := db.RegisterTypes(ctx, pool); err != nil {
		logger.Error("pgvector type registration failed", "err", err)
		os.Exit(1)
	}

	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer rdb.Close()
	if err := rdb.Ping(ctx).Err(); err != nil {
		logger.Error("redis ping failed", "err", err)
		os.Exit(1)
	}

	lis, err := net.Listen("tcp", ":8080")
	if err != nil {
		logger.Error("listen failed", "err", err)
		os.Exit(1)
	}
	grpcSrv := grpc.NewServer(grpc.StatsHandler(otelgrpc.NewServerHandler()))
	learnerv1.RegisterLearnerServiceServer(grpcSrv, grpcserver.New(pool, rdb, openAIKey))

	// HTTP REST: GET /suggestions + POST /api/corrections — port 8088
	restSrv := restapi.New(pool, rdb, logger)
	restapi.Start(restSrv, logger)

	ready.Store(true)
	logger.Info("learner server ready", "grpc", ":8080", "rest_http", ":8088", "health", ":8090")

	go func() {
		if err := grpcSrv.Serve(lis); err != nil {
			logger.Error("gRPC server error", "err", err)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down learner server")
	grpcSrv.GracefulStop()
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	healthSrv.Shutdown(shutCtx)
	restSrv.Shutdown(shutCtx)
	if telShutdown != nil {
		telShutdown(shutCtx)
	}
}

