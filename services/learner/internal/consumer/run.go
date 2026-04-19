package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/newsroom/learner/internal/db"
	"github.com/newsroom/learner/internal/embeddings"
	"github.com/newsroom/learner/internal/fastpath"
)

const (
	topicEditorCorrection = "editor.correction"
	topicModerationReject = "moderation.rejected"
	topicArticleApproved  = "article.approved" // switches to article.published when Sanity connector is built

	topicDLQCorrection = "editor.correction.dlq"
	topicDLQModeration = "moderation.rejected.dlq"
	topicDLQApproved   = "article.approved.dlq"

	maxRetries = 3
	retryBase  = 5 * time.Second
)

type correctionEvent struct {
	EventID        string `json:"event_id"`
	TraceID        string `json:"trace_id"`
	CorrectionID   string `json:"correction_id"`
	Market         string `json:"market"`
	CorrectionType string `json:"correction_type"`
	Reason         string `json:"reason"`
	OldValue       string `json:"old_value"`
	NewValue       string `json:"new_value"`
}

type rejectionEvent struct {
	EventID   string `json:"event_id"`
	TraceID   string `json:"trace_id"`
	ArticleID string `json:"article_id"`
	Market    string `json:"market"`
	Reason    string `json:"reason"`
}

type approvedEvent struct {
	EventID   string `json:"event_id"`
	TraceID   string `json:"trace_id"`
	ArticleID string `json:"article_id"`
	Market    string `json:"market"`
	Content   string `json:"content"`
}

// RunCorrections consumes editor.correction and moderation.rejected.
// For each event: fast-path Redis write + slow-path PostgreSQL log.
// Blocks until ctx is cancelled.
func RunCorrections(ctx context.Context, brokers []string, rdb *redis.Client, pool *pgxpool.Pool, logger *slog.Logger) error {
	cl, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ConsumerGroup("learner-ingest-corrections"),
		kgo.ConsumeTopics(topicEditorCorrection, topicModerationReject),
		kgo.DisableAutoCommit(),
	)
	if err != nil {
		return fmt.Errorf("corrections kafka client: %w", err)
	}
	defer cl.Close()

	dlq, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	if err != nil {
		return fmt.Errorf("dlq kafka client: %w", err)
	}
	defer dlq.Close()

	logger.Info("corrections consumer started", "topics", []string{topicEditorCorrection, topicModerationReject})

	for {
		fetches := cl.PollFetches(ctx)
		if fetches.IsClientClosed() || ctx.Err() != nil {
			return nil
		}
		fetches.EachError(func(t string, p int32, err error) {
			logger.Error("fetch error", "topic", t, "partition", p, "err", err)
		})

		var toCommit []*kgo.Record
		fetches.EachRecord(func(r *kgo.Record) {
			msgCtx := extractTrace(ctx, r)
			var processErr error

			for attempt := 0; attempt < maxRetries; attempt++ {
				if attempt > 0 {
					backoff := retryBase * (1 << (attempt - 1))
					select {
					case <-time.After(backoff):
					case <-ctx.Done():
						return
					}
				}
				switch r.Topic {
				case topicEditorCorrection:
					processErr = handleCorrection(msgCtx, r.Value, rdb, pool, logger)
				case topicModerationReject:
					processErr = handleRejection(msgCtx, r.Value, rdb, pool, logger)
				}
				if processErr == nil {
					break
				}
				logger.Warn("processing failed, retrying", "attempt", attempt+1, "err", processErr)
			}

			if processErr != nil {
				logger.Error("sending to DLQ", "topic", r.Topic, "err", processErr)
				dlqTopic := topicDLQCorrection
				if r.Topic == topicModerationReject {
					dlqTopic = topicDLQModeration
				}
				dlq.ProduceSync(ctx, &kgo.Record{Topic: dlqTopic, Key: r.Key, Value: r.Value, Headers: r.Headers})
			}
			toCommit = append(toCommit, r)
		})

		if len(toCommit) > 0 {
			if err := cl.CommitRecords(ctx, toCommit...); err != nil {
				logger.Error("corrections commit failed", "err", err)
			}
		}
	}
}

// RunPublished consumes article.approved and indexes new article embeddings.
// Blocks until ctx is cancelled.
func RunPublished(ctx context.Context, brokers []string, pool *pgxpool.Pool, openAIKey string, logger *slog.Logger) error {
	cl, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ConsumerGroup("learner-ingest-published"),
		kgo.ConsumeTopics(topicArticleApproved),
		kgo.DisableAutoCommit(),
	)
	if err != nil {
		return fmt.Errorf("published kafka client: %w", err)
	}
	defer cl.Close()

	dlq, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	if err != nil {
		return fmt.Errorf("dlq kafka client: %w", err)
	}
	defer dlq.Close()

	logger.Info("published consumer started", "topic", topicArticleApproved)

	for {
		fetches := cl.PollFetches(ctx)
		if fetches.IsClientClosed() || ctx.Err() != nil {
			return nil
		}
		fetches.EachError(func(t string, p int32, err error) {
			logger.Error("fetch error", "topic", t, "partition", p, "err", err)
		})

		var toCommit []*kgo.Record
		fetches.EachRecord(func(r *kgo.Record) {
			msgCtx := extractTrace(ctx, r)
			var processErr error

			for attempt := 0; attempt < maxRetries; attempt++ {
				if attempt > 0 {
					backoff := retryBase * (1 << (attempt - 1))
					select {
					case <-time.After(backoff):
					case <-ctx.Done():
						return
					}
				}
				processErr = handleApproved(msgCtx, r.Value, pool, openAIKey, logger)
				if processErr == nil {
					break
				}
				logger.Warn("indexing failed, retrying", "attempt", attempt+1, "err", processErr)
			}

			if processErr != nil {
				logger.Error("sending to DLQ", "topic", r.Topic, "err", processErr)
				dlq.ProduceSync(ctx, &kgo.Record{Topic: topicDLQApproved, Key: r.Key, Value: r.Value, Headers: r.Headers})
			}
			toCommit = append(toCommit, r)
		})

		if len(toCommit) > 0 {
			if err := cl.CommitRecords(ctx, toCommit...); err != nil {
				logger.Error("published commit failed", "err", err)
			}
		}
	}
}

func handleCorrection(ctx context.Context, data []byte, rdb *redis.Client, pool *pgxpool.Pool, logger *slog.Logger) error {
	var evt correctionEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return fmt.Errorf("unmarshal correction: %w", err)
	}
	if evt.Market == "" {
		return fmt.Errorf("missing market in correction event")
	}

	payload := map[string]interface{}{
		"correction_id":   evt.CorrectionID,
		"correction_type": evt.CorrectionType,
		"reason":          evt.Reason,
		"new_value":       evt.NewValue,
		"updated_at":      time.Now().UTC().Format(time.RFC3339),
	}

	// Fast path: Redis (immediate, 48h TTL)
	if err := fastpath.WriteCorrection(ctx, rdb, evt.Market, evt.CorrectionID, payload, logger); err != nil {
		return fmt.Errorf("fastpath write: %w", err)
	}

	// Slow path: PostgreSQL log
	if err := db.LogCorrection(ctx, pool, evt.CorrectionID, evt.Market, evt.CorrectionType, evt.Reason, evt.OldValue, evt.NewValue); err != nil {
		logger.Warn("slow-path correction log failed", "correction_id", evt.CorrectionID, "err", err)
		// Don't fail — fast path succeeded; slow path failure is non-fatal for the event
	} else {
		// DEL Redis key only after successful PostgreSQL write
		_ = fastpath.DeleteCorrection(ctx, rdb, evt.Market, evt.CorrectionID)
	}

	return nil
}

func handleRejection(ctx context.Context, data []byte, rdb *redis.Client, pool *pgxpool.Pool, logger *slog.Logger) error {
	var evt rejectionEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return fmt.Errorf("unmarshal rejection: %w", err)
	}
	if evt.Market == "" {
		return fmt.Errorf("missing market in rejection event")
	}

	payload := map[string]interface{}{
		"article_id": evt.ArticleID,
		"reason":     evt.Reason,
		"updated_at": time.Now().UTC().Format(time.RFC3339),
	}

	// Fast path: Redis
	correctionKey := "rejection:" + evt.ArticleID
	if err := fastpath.WriteCorrection(ctx, rdb, evt.Market, correctionKey, payload, logger); err != nil {
		return fmt.Errorf("fastpath rejection write: %w", err)
	}

	// Slow path: PostgreSQL
	if err := db.LogRejection(ctx, pool, evt.ArticleID, evt.Market, evt.Reason); err != nil {
		logger.Warn("slow-path rejection log failed", "article_id", evt.ArticleID, "err", err)
	}

	return nil
}

func handleApproved(ctx context.Context, data []byte, pool *pgxpool.Pool, openAIKey string, logger *slog.Logger) error {
	var evt approvedEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return fmt.Errorf("unmarshal approved: %w", err)
	}
	if evt.ArticleID == "" || evt.Market == "" || evt.Content == "" {
		return fmt.Errorf("missing required fields in approved event")
	}

	text := evt.Content
	if len(text) > 8000 {
		text = text[:8000]
	}

	embedding, err := embeddings.Generate(ctx, openAIKey, text)
	if err != nil {
		return fmt.Errorf("generate embedding: %w", err)
	}

	if err := db.UpsertEmbedding(ctx, pool, evt.ArticleID, evt.Market, embedding); err != nil {
		return fmt.Errorf("upsert embedding: %w", err)
	}

	logger.Info("article indexed", "article_id", evt.ArticleID, "market", evt.Market)
	return nil
}

func extractTrace(ctx context.Context, r *kgo.Record) context.Context {
	carrier := make(propagation.MapCarrier)
	for _, h := range r.Headers {
		carrier[h.Key] = string(h.Value)
	}
	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}
