package fastpath

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/redis/go-redis/v9"
)

const correctionTTL = 48 * time.Hour

var (
	correctionTTLRemaining = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "correction_ttl_remaining_seconds",
		Help: "Seconds until fast-path correction expires in Redis.",
	}, []string{"market"})

	correctionsWritten = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "corrections_written_total",
		Help: "Total correction entries written to Redis fast-path.",
	}, []string{"market"})
)

// WriteCorrection writes a correction payload to Redis with 48h TTL.
// Key pattern: corrections:<market>:<correction_id>
// Uses SET NX so DLQ replay does not overwrite an already-applied correction.
func WriteCorrection(ctx context.Context, rdb *redis.Client, market, correctionID string, payload map[string]interface{}, logger *slog.Logger) error {
	key := fmt.Sprintf("corrections:%s:%s", market, correctionID)

	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal correction payload: %w", err)
	}

	// NX: only write if key does not exist — idempotent on replay
	set, err := rdb.SetNX(ctx, key, raw, correctionTTL).Result()
	if err != nil {
		return fmt.Errorf("redis SETNX %s: %w", key, err)
	}
	if !set {
		logger.Debug("correction already exists, skipping (idempotent replay)", "key", key)
		return nil
	}

	correctionsWritten.WithLabelValues(market).Inc()
	correctionTTLRemaining.WithLabelValues(market).Set(correctionTTL.Seconds())
	logger.Info("correction written", "key", key, "ttl", correctionTTL)
	return nil
}

// DeleteCorrection removes a correction key after the slow path has applied it to PostgreSQL.
func DeleteCorrection(ctx context.Context, rdb *redis.Client, market, correctionID string) error {
	key := fmt.Sprintf("corrections:%s:%s", market, correctionID)
	return rdb.Del(ctx, key).Err()
}

// RefreshTTLMetrics scans correction keys and updates the gauge with actual remaining TTL.
func RefreshTTLMetrics(ctx context.Context, rdb *redis.Client, markets []string) {
	for _, market := range markets {
		pattern := fmt.Sprintf("corrections:%s:*", market)
		var minTTL float64 = correctionTTL.Seconds()

		iter := rdb.Scan(ctx, 0, pattern, 100).Iterator()
		for iter.Next(ctx) {
			d, err := rdb.TTL(ctx, iter.Val()).Result()
			if err == nil && d > 0 && d.Seconds() < minTTL {
				minTTL = d.Seconds()
			}
		}
		correctionTTLRemaining.WithLabelValues(market).Set(minTTL)
	}
}
