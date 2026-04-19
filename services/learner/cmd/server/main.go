package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
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

	// HTTP REST endpoint for frontend: GET /suggestions?market=italy&limit=10
	// Exposed on port 8088 so Caddy can route /api/suggestions/* to it.
	suggestionsSrv := startSuggestionsServer(pool)

	ready.Store(true)
	logger.Info("learner server ready", "grpc", ":8080", "health", ":8090", "suggestions_http", ":8088")

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
	suggestionsSrv.Shutdown(shutCtx)
	if telShutdown != nil {
		telShutdown(shutCtx)
	}
}

func startSuggestionsServer(pool *pgxpool.Pool) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/suggestions", func(w http.ResponseWriter, r *http.Request) {
		market := r.URL.Query().Get("market")
		if market == "" {
			http.Error(w, `{"error":"market required"}`, http.StatusBadRequest)
			return
		}
		limit := 10
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 50 {
				limit = n
			}
		}
		rows, err := db.GetTopicSuggestions(r.Context(), pool, market, limit)
		if err != nil {
			http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(rows)
	})
	srv := &http.Server{Addr: ":8088", Handler: mux}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("suggestions server error", "err", err)
		}
	}()
	return srv
}
