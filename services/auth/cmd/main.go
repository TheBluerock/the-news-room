package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	casbinx "github.com/newsroom/auth/internal/casbin"
	"github.com/newsroom/auth/internal/gdpr"
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

	// ── GDPR deletion event publisher (Phase K2) ─────────────────────────────
	// redpanda_brokers is optional: if missing we run with a nil publisher and
	// log a warning. The DELETE handler still works (local anonymisation), but
	// downstream services won't be notified — degraded mode for dev/test only.
	var gdprPub gdpr.Publisher
	if brokers, err := secrets.Require("redpanda_brokers"); err == nil && brokers != "" {
		seeds := strings.Split(brokers, ",")
		if p, err := gdpr.NewKafkaPublisher(seeds); err != nil {
			logger.Warn("gdpr publisher init failed, running without event publish", "err", err)
		} else {
			gdprPub = p
		}
	} else {
		logger.Warn("redpanda_brokers absent, GDPR deletion events will not be published")
	}

	// ── GDPR deletion lag cron — Phase K2 ────────────────────────────────────
	// Every 6h: emit metric of pending deletions older than 25 days. Alertmanager
	// fires GDPRDeletionLag well before the 30-day legal cutoff.
	go runDeletionLagCron(ctx, db, logger)

	// ── HTTP server (:8080) ──────────────────────────────────────────────────
	mux := server.NewHTTP(jwtMgr, db, rdb, enforcer, logger, gdprPub)
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

// gdprDeletionsPending tracks user_deletions rows whose requested_at is older
// than the 25-day soft alert threshold (Phase K2). Alertmanager rule fires
// when this gauge > 0.
var gdprDeletionsPending = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "gdpr_user_deletions_pending_total",
	Help: "Number of GDPR deletion requests older than 25 days without all services completed.",
})

// runDeletionLagCron periodically refreshes gdprDeletionsPending. 6h cadence
// is enough granularity — the 30-day legal cutoff is the only hard deadline.
func runDeletionLagCron(ctx context.Context, db *pgxpool.Pool, logger *slog.Logger) {
	const threshold = 25 * 24 * time.Hour
	const interval = 6 * time.Hour

	check := func() {
		ids, err := gdpr.PendingOlderThan(ctx, db, threshold)
		if err != nil {
			logger.Warn("gdpr lag cron query failed", "err", err)
			return
		}
		gdprDeletionsPending.Set(float64(len(ids)))
		if len(ids) > 0 {
			logger.Warn("GDPR deletions exceeded 25-day soft cutoff",
				"count", len(ids), "user_ids_sample", ids[:min(len(ids), 5)])
		}
	}

	check() // first run on startup so alert state stabilises quickly.
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			check()
		}
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
