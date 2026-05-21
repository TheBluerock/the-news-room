// Package gdpr handles user data deletion (Phase K2).
//
// Worldwide 30-day deletion right, uniform across markets — owner decision 2026-05-21.
// Owns:
//   1. The DELETE /api/user/data handler (Authorization: Bearer required).
//   2. Local anonymisation of auth_svc.users + audit_log via sentinel UUID.
//   3. user.data.deletion.requested event production so other services
//      can do their own anonymisation work.
//   4. The auth_svc.user_deletions ledger that the lag cron polls.
package gdpr

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/twmb/franz-go/pkg/kgo"
)

const TopicDeletionRequested = "user.data.deletion.requested"

// Publisher emits user.data.deletion.requested events. Defined as an interface
// so the HTTP layer can be tested against a stub without spinning a Kafka.
type Publisher interface {
	Publish(ctx context.Context, evt Event) error
}

// Event matches infra/schemas/user.data.deletion.requested.json. Required
// fields: event_id, trace_id, user_id, requested_by, timestamp.
type Event struct {
	EventID     string `json:"event_id"`
	TraceID     string `json:"trace_id"`
	UserID      string `json:"user_id"`
	RequestedBy string `json:"requested_by"`
	Timestamp   string `json:"timestamp"`
}

// NewEvent builds a fully-populated Event for the given subject.
// requestedBy = userID when the user requests their own deletion; admin-initiated
// deletions should pass the admin's user_id instead.
func NewEvent(userID, requestedBy, traceID string) Event {
	return Event{
		EventID:     uuid.NewString(),
		TraceID:     traceID,
		UserID:      userID,
		RequestedBy: requestedBy,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
}

// kafkaPublisher is the franz-go-backed concrete implementation.
type kafkaPublisher struct {
	client *kgo.Client
}

// NewKafkaPublisher constructs a Publisher that produces synchronously to ``brokers``.
// Sync produce is acceptable here: DELETE /api/user/data is a low-frequency,
// high-importance call where the user must see "accepted" only after the event
// is durably written.
func NewKafkaPublisher(brokers []string) (Publisher, error) {
	cl, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	if err != nil {
		return nil, fmt.Errorf("kafka client: %w", err)
	}
	return &kafkaPublisher{client: cl}, nil
}

func (p *kafkaPublisher) Publish(ctx context.Context, evt Event) error {
	data, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("marshal deletion event: %w", err)
	}
	res := p.client.ProduceSync(ctx, &kgo.Record{
		Topic: TopicDeletionRequested,
		Key:   []byte(evt.UserID),
		Value: data,
	})
	if err := res[0].Err; err != nil {
		return fmt.Errorf("produce deletion event: %w", err)
	}
	return nil
}

// Close releases the underlying Kafka client. Safe to call on stub publishers
// (no-op fallback via interface satisfaction at call site).
func (p *kafkaPublisher) Close() { p.client.Close() }
