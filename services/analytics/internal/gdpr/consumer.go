// Package gdpr handles the analytics-side of GDPR deletion (Phase K2).
//
// Subscribes to user.data.deletion.requested events. For each event, NULLs
// out PII references owned by analytics (editorial_calendar.created_by),
// then stamps the user_deletions ledger to signal completion.
package gdpr

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/twmb/franz-go/pkg/kgo"
)

const (
	Topic        = "user.data.deletion.requested"
	TopicDLQ     = "user.data.deletion.requested.dlq"
	ConsumerGroup = "analytics-gdpr"
	ServiceName   = "analytics"
	maxRetries    = 3
)

// Event mirrors the schema in infra/schemas/user.data.deletion.requested.json.
type Event struct {
	EventID     string `json:"event_id"`
	TraceID     string `json:"trace_id"`
	UserID      string `json:"user_id"`
	RequestedBy string `json:"requested_by"`
	Timestamp   string `json:"timestamp"`
}

// Run consumes the deletion topic until ctx is cancelled. Errors after retries
// are routed to the DLQ topic rather than blocking the consumer.
func Run(ctx context.Context, brokers []string, db *pgxpool.Pool, logger *slog.Logger) error {
	cl, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ConsumerGroup(ConsumerGroup),
		kgo.ConsumeTopics(Topic),
		kgo.DisableAutoCommit(),
	)
	if err != nil {
		return fmt.Errorf("kafka client: %w", err)
	}
	defer cl.Close()

	dlq, _ := kgo.NewClient(kgo.SeedBrokers(brokers...))
	defer dlq.Close()

	logger.Info("gdpr deletion consumer started", "topic", Topic, "group", ConsumerGroup)

	for {
		fetches := cl.PollFetches(ctx)
		if fetches.IsClientClosed() || ctx.Err() != nil {
			return nil
		}

		var committable []*kgo.Record
		fetches.EachRecord(func(r *kgo.Record) {
			var processErr error
			for attempt := 0; attempt < maxRetries; attempt++ {
				if attempt > 0 {
					time.Sleep(time.Duration(1<<attempt) * time.Second)
				}
				processErr = handle(ctx, r.Value, db, logger)
				if processErr == nil {
					break
				}
			}
			if processErr != nil {
				logger.Error("gdpr deletion handling failed after retries, routing to DLQ",
					"err", processErr, "key", string(r.Key))
				dlq.ProduceSync(ctx, &kgo.Record{
					Topic: TopicDLQ, Key: r.Key, Value: r.Value, Headers: r.Headers,
				})
			}
			committable = append(committable, r)
		})

		if len(committable) > 0 {
			cl.CommitRecords(ctx, committable...)
		}
	}
}

// handle does the analytics-side anonymisation for one deletion event.
func handle(ctx context.Context, data []byte, db *pgxpool.Pool, logger *slog.Logger) error {
	var evt Event
	if err := json.Unmarshal(data, &evt); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	if evt.UserID == "" {
		return fmt.Errorf("event missing user_id")
	}

	// Nullify created_by in editorial_calendar. We don't delete the row — the
	// calendar entry itself is editorial metadata that survives the user's
	// identity. created_by becoming NULL means "originator anonymised".
	if _, err := db.Exec(ctx, `
		UPDATE analytics_svc.editorial_calendar
		SET created_by = NULL
		WHERE created_by = $1::uuid
	`, evt.UserID); err != nil {
		return fmt.Errorf("nullify editorial_calendar.created_by: %w", err)
	}

	// Stamp completion in the ledger. JSONB merge is idempotent — if "analytics"
	// is already a key we just refresh the timestamp.
	if _, err := db.Exec(ctx, `
		UPDATE user_deletions
		SET services_completed = services_completed || jsonb_build_object($2::text, to_jsonb(now())),
		    completed_at       = CASE
		        WHEN (services_completed || jsonb_build_object($2::text, true)) ?& ARRAY['auth', 'analytics']::text[]
		            THEN now()
		        ELSE completed_at
		    END
		WHERE user_id = $1::uuid
	`, evt.UserID, ServiceName); err != nil {
		// Soft-fail: the ledger row might not yet be visible (race) or might
		// have been wiped manually. Log + carry on — at worst the cron alerts.
		logger.Warn("stamp ledger failed", "user_id", evt.UserID, "err", err)
	}

	logger.Info("gdpr anonymisation done (analytics)", "user_id", evt.UserID)
	return nil
}
