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

	"github.com/newsroom/sanity/internal/client"
	"github.com/newsroom/sanity/internal/consumer"
	"github.com/newsroom/sanity/internal/telemetry"
	"github.com/newsroom/sanity/internal/vault"
	"github.com/newsroom/sanity/internal/webhook"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	var ready atomic.Bool

	telShutdown, metricsHandler, err := telemetry.Init(ctx, "sanity-connector")
	if err != nil {
		logger.Warn("telemetry init failed, continuing without observability", "err", err)
		metricsHandler = http.NotFoundHandler()
	}

	secrets, err := vault.Load("sanity")
	if err != nil {
		logger.Error("vault load failed", "err", err)
		os.Exit(1)
	}

	projectID, err := secrets.Require("sanity_project_id")
	if err != nil {
		logger.Error("missing sanity_project_id", "err", err)
		os.Exit(1)
	}
	dataset, err := secrets.Require("sanity_dataset")
	if err != nil {
		logger.Error("missing sanity_dataset", "err", err)
		os.Exit(1)
	}
	apiToken, err := secrets.Require("sanity_api_token")
	if err != nil {
		logger.Error("missing sanity_api_token", "err", err)
		os.Exit(1)
	}
	webhookSecret, err := secrets.Require("sanity_webhook_secret")
	if err != nil {
		logger.Error("missing sanity_webhook_secret", "err", err)
		os.Exit(1)
	}
	redpandaBrokers, err := secrets.Require("redpanda_brokers")
	if err != nil {
		logger.Error("missing redpanda_brokers", "err", err)
		os.Exit(1)
	}

	brokers := strings.Split(redpandaBrokers, ",")
	sanityClient := client.New(projectID, dataset, apiToken)

	webhookSrv, err := webhook.NewServer(brokers, webhookSecret, logger.With("component", "webhook"))
	if err != nil {
		logger.Error("webhook server init failed", "err", err)
		os.Exit(1)
	}
	defer webhookSrv.Close()

	// article.approved consumer
	go func() {
		if err := consumer.Run(ctx, brokers, sanityClient, logger.With("component", "consumer")); err != nil && ctx.Err() == nil {
			logger.Error("consumer exited", "err", err)
		}
	}()

	// Webhook HTTP server (:8088) — receives Sanity publish events
	webhookHTTP := &http.Server{
		Addr:    ":8088",
		Handler: webhookSrv.Handler(),
	}
	go func() {
		if err := webhookHTTP.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("webhook HTTP server error", "err", err)
		}
	}()

	// Health/metrics server (:8090)
	healthSrv := startHealthServer(&ready, metricsHandler)

	ready.Store(true)
	logger.Info("sanity connector ready", "webhook", ":8088", "health", ":8090")

	<-ctx.Done()
	logger.Info("shutting down")

	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = webhookHTTP.Shutdown(shutCtx)
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
