package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	analyticsv1 "github.com/newsroom/proto/analytics/v1"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"

	grpcserver "github.com/newsroom/analytics/internal/grpc"
	"github.com/newsroom/analytics/internal/telemetry"
	"github.com/newsroom/analytics/internal/tracker"
	"github.com/newsroom/analytics/internal/trends"
	"github.com/newsroom/analytics/internal/vault"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	var ready atomic.Bool

	telShutdown, metricsHandler, err := telemetry.Init(ctx, "analytics")
	if err != nil {
		logger.Warn("telemetry init failed, continuing without observability", "err", err)
		metricsHandler = http.NotFoundHandler()
	}
	healthSrv := startHealthServer(&ready, metricsHandler)

	secrets, err := vault.Load("analytics")
	if err != nil {
		logger.Error("vault load failed", "err", err)
		os.Exit(1)
	}

	postgresDSN, err := secrets.Require("postgres_dsn")
	if err != nil {
		logger.Error("missing postgres_dsn", "err", err)
		os.Exit(1)
	}
	redpandaBrokers, err := secrets.Require("redpanda_brokers")
	if err != nil {
		logger.Error("missing redpanda_brokers", "err", err)
		os.Exit(1)
	}

	db, err := pgxpool.New(ctx, postgresDSN)
	if err != nil {
		logger.Error("postgres connect failed", "err", err)
		os.Exit(1)
	}
	defer db.Close()
	if err := db.Ping(ctx); err != nil {
		logger.Error("postgres ping failed", "err", err)
		os.Exit(1)
	}

	brokers := strings.Split(redpandaBrokers, ",")

	lis, err := net.Listen("tcp", ":8080")
	if err != nil {
		logger.Error("listen failed", "err", err)
		os.Exit(1)
	}

	grpcSrv := grpc.NewServer(grpc.StatsHandler(otelgrpc.NewServerHandler()))
	analyticsv1.RegisterAnalyticsServiceServer(grpcSrv, grpcserver.New(db))

	pub, err := trends.NewPublisher(db, brokers, 15*time.Minute, logger)
	if err != nil {
		logger.Error("trending publisher init failed", "err", err)
		os.Exit(1)
	}
	defer pub.Close()
	go pub.Run(ctx)

	go func() {
		if err := tracker.RunPublishedConsumer(ctx, brokers, db, logger); err != nil && ctx.Err() == nil {
			logger.Error("published consumer exited", "err", err)
		}
	}()

	ready.Store(true)
	logger.Info("analytics service ready", "grpc", ":8080", "health", ":8090")

	go func() {
		if err := grpcSrv.Serve(lis); err != nil {
			logger.Error("gRPC server error", "err", err)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")
	grpcSrv.GracefulStop()
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	healthSrv.Shutdown(shutCtx)
	if telShutdown != nil {
		telShutdown(shutCtx)
	}
}

func startHealthServer(ready *atomic.Bool, metricsHandler http.Handler) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", metricsHandler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !ready.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{"status": "not ready"})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
	})
	srv := &http.Server{Addr: ":8090", Handler: mux}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("health server error", "err", err)
		}
	}()
	return srv
}
