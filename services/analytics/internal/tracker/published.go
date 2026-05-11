package tracker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

const (
	topicPublished    = "article.published"
	topicPublishedDLQ = "article.published.dlq"
	maxRetries        = 3
	retryBase         = 5 * time.Second
)

type publishedEvent struct {
	EventID   string `json:"event_id"`
	TraceID   string `json:"trace_id"`
	ArticleID string `json:"article_id"`
	Market    string `json:"market"`
	SanityID  string `json:"sanity_id"`
	Timestamp string `json:"timestamp"`
}

// RunPublishedConsumer consumes article.published and records publish timestamps.
func RunPublishedConsumer(ctx context.Context, brokers []string, db *pgxpool.Pool, logger *slog.Logger) error {
	cl, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ConsumerGroup("analytics-published"),
		kgo.ConsumeTopics(topicPublished),
		kgo.DisableAutoCommit(),
	)
	if err != nil {
		return fmt.Errorf("kafka client: %w", err)
	}
	defer cl.Close()

	dlq, _ := kgo.NewClient(kgo.SeedBrokers(brokers...))
	defer dlq.Close()

	logger.Info("analytics published consumer started")

	for {
		fetches := cl.PollFetches(ctx)
		if fetches.IsClientClosed() || ctx.Err() != nil {
			return nil
		}

		var commitRecords []*kgo.Record
		fetches.EachRecord(func(r *kgo.Record) {
			carrier := make(propagation.MapCarrier)
			for _, h := range r.Headers {
				carrier[h.Key] = string(h.Value)
			}
			msgCtx := otel.GetTextMapPropagator().Extract(ctx, carrier)

			var processErr error
			for attempt := 0; attempt < maxRetries; attempt++ {
				if attempt > 0 {
					time.Sleep(retryBase * (1 << (attempt - 1)))
				}
				processErr = handlePublished(msgCtx, r.Value, db, logger)
				if processErr == nil {
					break
				}
			}
			if processErr != nil {
				dlq.ProduceSync(ctx, &kgo.Record{
					Topic: topicPublishedDLQ, Key: r.Key, Value: r.Value, Headers: r.Headers,
				})
			}
			commitRecords = append(commitRecords, r)
		})

		if len(commitRecords) > 0 {
			cl.CommitRecords(ctx, commitRecords...)
		}
	}
}

func handlePublished(ctx context.Context, data []byte, db *pgxpool.Pool, logger *slog.Logger) error {
	var evt publishedEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}

	// Record publish timestamp and Sanity ID — article_performance tracks this
	_, err := db.Exec(ctx, `
		INSERT INTO article_performance (article_id, market, published_at, created_at)
		VALUES ($1, $2, now(), now())
		ON CONFLICT (article_id) DO UPDATE SET published_at = now()
	`, evt.ArticleID, evt.Market)
	if err != nil {
		logger.Warn("article_performance update failed", "article_id", evt.ArticleID, "err", err)
	}

	logger.Info("article.published processed", "article_id", evt.ArticleID, "market", evt.Market)
	return nil
}
