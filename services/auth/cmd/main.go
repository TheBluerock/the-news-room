package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"google.golang.org/grpc"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	var ready atomic.Bool
	healthSrv := startHealthServer(&ready)

	// Load secrets from Vault Agent sidecar (/vault/secrets/) or env fallback
	// secrets := loadSecrets()

	// TODO: connect to PostgreSQL (Casbin adapter), Redis (JWT blocklist)
	// TODO: start gRPC server on :8080 with AuthService implementation
	// TODO: register OTel tracer

	lis, err := net.Listen("tcp", ":8080")
	if err != nil {
		logger.Error("failed to listen", "err", err)
		os.Exit(1)
	}
	grpcSrv := grpc.NewServer()
	// authv1.RegisterAuthServiceServer(grpcSrv, &authServer{})

	ready.Store(true)
	logger.Info("auth service ready", "grpc", ":8080", "health", ":8090")

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
}

func startHealthServer(ready *atomic.Bool) *http.Server {
	mux := http.NewServeMux()

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

	// Prometheus metrics endpoint
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		// TODO: wire prometheus handler
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{Addr: ":8090", Handler: mux}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("health server error", "err", err)
		}
	}()
	return srv
}
