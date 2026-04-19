package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	casbinx "github.com/newsroom/auth/internal/casbin"
	jwtpkg "github.com/newsroom/auth/internal/jwt"
	"github.com/newsroom/auth/internal/server"
	"github.com/newsroom/auth/internal/store"
	"github.com/newsroom/auth/internal/telemetry"
	"github.com/newsroom/auth/internal/vault"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	var ready atomic.Bool

	// ── Telemetry ────────────────────────────────────────────────────────────
	telShutdown, metricsHandler, err := telemetry.Init(ctx, "auth")
	if err != nil {
		logger.Warn("telemetry init failed, continuing without observability", "err", err)
		metricsHandler = http.NotFoundHandler()
	}
	healthSrv := startHealthServer(&ready, metricsHandler)

	// ── Vault ────────────────────────────────────────────────────────────────
	secrets, err := vault.Load("auth")
	if err != nil {
		logger.Error("vault load failed", "err", err)
		os.Exit(1)
	}

	jwtKey, err := secrets.Require("jwt_private_key")
	if err != nil {
		logger.Error("missing jwt_private_key", "err", err)
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

	// ── JWT ──────────────────────────────────────────────────────────────────
	jwtMgr, err := jwtpkg.NewManager(jwtKey)
	if err != nil {
		logger.Error("jwt init failed", "err", err)
		os.Exit(1)
	}

	// ── PostgreSQL ───────────────────────────────────────────────────────────
	db, err := store.ConnectPG(ctx, postgresDSN)
	if err != nil {
		logger.Error("postgres connect failed", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	// ── Redis ────────────────────────────────────────────────────────────────
	rdb, err := store.ConnectRedis(ctx, redisAddr)
	if err != nil {
		logger.Error("redis connect failed", "err", err)
		os.Exit(1)
	}
	defer rdb.Close()

	// ── Casbin ───────────────────────────────────────────────────────────────
	casbinRules, err := store.LoadCasbinRules(ctx, db)
	if err != nil {
		logger.Error("casbin rules load failed", "err", err)
		os.Exit(1)
	}
	enforcer, err := casbinx.NewEnforcer(casbinRules)
	if err != nil {
		logger.Error("casbin enforcer init failed", "err", err)
		os.Exit(1)
	}

	// ── HTTP server (:8080) ──────────────────────────────────────────────────
	mux := server.NewHTTP(jwtMgr, db, rdb, enforcer, logger)
	httpSrv := server.StartHTTP(otelhttp.NewHandler(mux, "auth"), ":8080")

	ready.Store(true)
	logger.Info("auth service ready", "http", ":8080", "health", ":8090")

	<-ctx.Done()
	logger.Info("shutting down")

	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	httpSrv.Shutdown(shutCtx)
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
