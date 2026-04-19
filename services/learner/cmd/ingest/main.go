package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/newsroom/learner/internal/consumer"
	"github.com/newsroom/learner/internal/db"
	"github.com/newsroom/learner/internal/fastpath"
	"github.com/newsroom/learner/internal/health"
	"github.com/newsroom/learner/internal/scraper"
	"github.com/newsroom/learner/internal/telemetry"
	"github.com/newsroom/learner/internal/vault"
)

var markets = []string{"italy", "usa", "china"}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	var ready atomic.Bool

	telShutdown, metricsHandler, err := telemetry.Init(ctx, "learner-ingest")
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
	redpandaBrokers, err := secrets.Require("redpanda_brokers")
	if err != nil {
		logger.Error("missing redpanda_brokers", "err", err)
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

	brokers := strings.Split(redpandaBrokers, ",")

	// Load market configs and start one scraper goroutine per market
	configDir := envOr("CONFIG_DIR", "config")
	for _, market := range markets {
		cfgPath := filepath.Join(configDir, "sources_"+market+".yaml")
		cfg, err := scraper.LoadConfig(cfgPath)
		if err != nil {
			logger.Error("failed to load market config", "market", market, "path", cfgPath, "err", err)
			continue
		}
		go scraper.RunScraper(ctx, cfg, pool, openAIKey, logger.With("component", "scraper", "market", market))
	}

	// Start Kafka consumers
	go func() {
		if err := consumer.RunCorrections(ctx, brokers, rdb, pool, logger.With("component", "consumer.corrections")); err != nil && ctx.Err() == nil {
			logger.Error("corrections consumer exited", "err", err)
		}
	}()

	go func() {
		if err := consumer.RunPublished(ctx, brokers, pool, openAIKey, logger.With("component", "consumer.published")); err != nil && ctx.Err() == nil {
			logger.Error("published consumer exited", "err", err)
		}
	}()

	// Start TTL metrics refresh ticker (every 5 minutes)
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				fastpath.RefreshTTLMetrics(ctx, rdb, markets)
			}
		}
	}()

	ready.Store(true)
	logger.Info("learner-ingest ready", "markets", markets, "brokers", brokers)

	<-ctx.Done()
	logger.Info("shutting down learner-ingest")
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	healthSrv.Shutdown(shutCtx)
	if telShutdown != nil {
		telShutdown(shutCtx)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
