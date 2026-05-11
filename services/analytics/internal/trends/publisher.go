package trends

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

const topicTrending = "topic.trending"

type trendingEvent struct {
	EventID   string `json:"event_id"`
	TraceID   string `json:"trace_id"`
	Market    string `json:"market"`
	TopicID   string `json:"topic_id"`
	TopicName string `json:"topic_name"`
	Timestamp string `json:"timestamp"`
}

type calendarEntry struct {
	id        string
	market    string
	topicID   string
	topicName string
}

// Publisher dispatches due editorial_calendar entries as topic.trending events.
type Publisher struct {
	db       *pgxpool.Pool
	producer *kgo.Client
	interval time.Duration
	logger   *slog.Logger
}

func NewPublisher(db *pgxpool.Pool, brokers []string, interval time.Duration, logger *slog.Logger) (*Publisher, error) {
	cl, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	if err != nil {
		return nil, fmt.Errorf("kafka producer: %w", err)
	}
	return &Publisher{db: db, producer: cl, interval: interval, logger: logger}, nil
}

func (p *Publisher) Close() { p.producer.Close() }

// Run dispatches due calendar entries on every tick until ctx is cancelled.
func (p *Publisher) Run(ctx context.Context) {
	p.dispatch(ctx)
	t := time.NewTicker(p.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.dispatch(ctx)
		}
	}
}

func (p *Publisher) dispatch(ctx context.Context) {
	entries, err := p.fetchDue(ctx)
	if err != nil {
		p.logger.Error("fetch due calendar entries failed", "err", err)
		return
	}
	if len(entries) == 0 {
		return
	}

	traceCtx, span := otel.Tracer("analytics/trends").Start(ctx, "DispatchCalendar")
	defer span.End()

	carrier := make(propagation.MapCarrier)
	otel.GetTextMapPropagator().Inject(traceCtx, carrier)
	traceID := carrier["traceparent"]

	var headers []kgo.RecordHeader
	for k, v := range carrier {
		headers = append(headers, kgo.RecordHeader{Key: k, Value: []byte(v)})
	}

	for _, e := range entries {
		evt := trendingEvent{
			EventID:   uuid.New().String(),
			TraceID:   traceID,
			Market:    e.market,
			TopicID:   e.topicID,
			TopicName: e.topicName,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}
		data, err := json.Marshal(evt)
		if err != nil {
			p.logger.Error("marshal trending event", "calendar_id", e.id, "err", err)
			continue
		}

		res := p.producer.ProduceSync(ctx, &kgo.Record{
			Topic:   topicTrending,
			Value:   data,
			Key:     []byte(e.market + ":" + e.topicID),
			Headers: headers,
		})
		if err := res[0].Err; err != nil {
			p.logger.Error("produce trending event", "calendar_id", e.id, "err", err)
			continue
		}

		if err := p.markDispatched(ctx, e.id); err != nil {
			p.logger.Error("mark dispatched failed", "calendar_id", e.id, "err", err)
		}
		p.logger.Info("topic.trending dispatched", "market", e.market, "topic", e.topicName, "calendar_id", e.id)
	}
}

func (p *Publisher) fetchDue(ctx context.Context) ([]calendarEntry, error) {
	rows, err := p.db.Query(ctx, `
		SELECT id::text, market, topic_id, topic_name
		FROM editorial_calendar
		WHERE scheduled_at <= now()
		  AND dispatched = false
		ORDER BY scheduled_at ASC
		LIMIT 50
	`)
	if err != nil {
		return nil, fmt.Errorf("query editorial_calendar: %w", err)
	}
	defer rows.Close()

	var entries []calendarEntry
	for rows.Next() {
		var e calendarEntry
		if err := rows.Scan(&e.id, &e.market, &e.topicID, &e.topicName); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (p *Publisher) markDispatched(ctx context.Context, id string) error {
	_, err := p.db.Exec(ctx, `
		UPDATE editorial_calendar
		SET dispatched = true, dispatched_at = now()
		WHERE id::text = $1
	`, id)
	return err
}
