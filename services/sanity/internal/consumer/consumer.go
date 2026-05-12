package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/newsroom/sanity/internal/client"
)

const (
	topicApproved    = "article.approved"
	topicApprovedDLQ = "article.approved.dlq"
	maxRetries       = 3
	retryBase        = 5 * time.Second
)

type approvedEvent struct {
	EventID      string   `json:"event_id"`
	TraceID      string   `json:"trace_id"`
	ArticleID    string   `json:"article_id"`
	Market       string   `json:"market"`
	Language     string   `json:"language"`
	Content      string   `json:"content"`
	Title        string   `json:"title"`
	Excerpt      string   `json:"excerpt"`
	Section      string   `json:"section"`
	Author       string   `json:"author"`
	Tags         []string `json:"tags"`
	Slug         string   `json:"slug"`
	QualityScore float64  `json:"quality_score"`
	Timestamp    string   `json:"timestamp"`
}

// Run consumes article.approved and creates Sanity draft documents.
// Blocks until ctx is cancelled.
func Run(ctx context.Context, brokers []string, sanityClient *client.Client, logger *slog.Logger) error {
	cl, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ConsumerGroup("sanity-connector"),
		kgo.ConsumeTopics(topicApproved),
		kgo.DisableAutoCommit(),
	)
	if err != nil {
		return fmt.Errorf("kafka client: %w", err)
	}
	defer cl.Close()

	dlq, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	if err != nil {
		return fmt.Errorf("dlq kafka client: %w", err)
	}
	defer dlq.Close()

	logger.Info("sanity consumer started", "topic", topicApproved)

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
			carrier := make(propagation.MapCarrier)
			for _, h := range r.Headers {
				carrier[h.Key] = string(h.Value)
			}
			msgCtx := otel.GetTextMapPropagator().Extract(ctx, carrier)

			var processErr error
			for attempt := 0; attempt < maxRetries; attempt++ {
				if attempt > 0 {
					select {
					case <-time.After(retryBase * (1 << (attempt - 1))):
					case <-ctx.Done():
						return
					}
				}
				processErr = handle(msgCtx, r.Value, sanityClient, logger)
				if processErr == nil {
					break
				}
				logger.Warn("sanity draft creation failed, retrying", "attempt", attempt+1, "err", processErr)
			}

			if processErr != nil {
				logger.Error("sending to DLQ", "err", processErr)
				dlq.ProduceSync(ctx, &kgo.Record{
					Topic: topicApprovedDLQ, Key: r.Key, Value: r.Value, Headers: r.Headers,
				})
			}
			toCommit = append(toCommit, r)
		})

		if len(toCommit) > 0 {
			if err := cl.CommitRecords(ctx, toCommit...); err != nil {
				logger.Error("commit failed", "err", err)
			}
		}
	}
}

func handle(ctx context.Context, data []byte, sanityClient *client.Client, logger *slog.Logger) error {
	var evt approvedEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	if evt.ArticleID == "" || evt.Market == "" {
		return fmt.Errorf("missing article_id or market")
	}

	doc := client.ArticleDoc{
		ArticleID:    evt.ArticleID,
		Market:       evt.Market,
		Language:     evt.Language,
		Content:      evt.Content,
		Title:        evt.Title,
		Excerpt:      evt.Excerpt,
		Section:      evt.Section,
		Byline:       evt.Author,
		Tags:         evt.Tags,
		Slug:         client.NewSlug(evt.Slug),
		QualityScore: evt.QualityScore,
		ApprovedAt:   evt.Timestamp,
	}

	if err := sanityClient.CreateDraft(ctx, doc); err != nil {
		return fmt.Errorf("create draft article_id=%s: %w", evt.ArticleID, err)
	}

	logger.Info("sanity draft created", "article_id", evt.ArticleID, "market", evt.Market)
	return nil
}
