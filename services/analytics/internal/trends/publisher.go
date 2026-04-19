package trends

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

const topicTrending = "topic.trending"

type trendingEvent struct {
	EventID    string  `json:"event_id"`
	TraceID    string  `json:"trace_id"`
	Market     string  `json:"market"`
	TopicID    string  `json:"topic_id"`
	TopicName  string  `json:"topic_name"`
	TrendScore float64 `json:"trend_score"`
	Timestamp  string  `json:"timestamp"`
}

var markets = []string{"italy", "usa", "china"}

// Publisher periodically publishes topic.trending events for each market.
type Publisher struct {
	db       *pgxpool.Pool
	rdb      *redis.Client
	producer *kgo.Client
	interval time.Duration
	logger   *slog.Logger
}

func NewPublisher(db *pgxpool.Pool, rdb *redis.Client, brokers []string, interval time.Duration, logger *slog.Logger) (*Publisher, error) {
	cl, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	if err != nil {
		return nil, fmt.Errorf("kafka producer: %w", err)
	}
	return &Publisher{db: db, rdb: rdb, producer: cl, interval: interval, logger: logger}, nil
}

func (p *Publisher) Close() { p.producer.Close() }

// Run publishes trending topics on a ticker until ctx is cancelled.
func (p *Publisher) Run(ctx context.Context) {
	// Publish immediately on startup, then on interval
	p.publishAll(ctx)
	t := time.NewTicker(p.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.publishAll(ctx)
		}
	}
}

func (p *Publisher) publishAll(ctx context.Context) {
	for _, market := range markets {
		if err := p.publishMarket(ctx, market); err != nil {
			p.logger.Error("failed to publish trending", "market", market, "err", err)
		}
	}
}

func (p *Publisher) publishMarket(ctx context.Context, market string) error {
	topics, err := p.fetchTopTopics(ctx, market, 3)
	if err != nil {
		return err
	}

	traceCtx, span := otel.Tracer("analytics/trends").Start(ctx, "PublishTrending")
	defer span.End()

	carrier := make(propagation.MapCarrier)
	otel.GetTextMapPropagator().Inject(traceCtx, carrier)
	traceID := carrier["traceparent"]

	var records []*kgo.Record
	for _, t := range topics {
		evt := trendingEvent{
			EventID:    uuid.New().String(),
			TraceID:    traceID,
			Market:     market,
			TopicID:    t.id,
			TopicName:  t.name,
			TrendScore: t.score,
			Timestamp:  time.Now().UTC().Format(time.RFC3339),
		}
		data, err := json.Marshal(evt)
		if err != nil {
			continue
		}

		var headers []kgo.RecordHeader
		for k, v := range carrier {
			headers = append(headers, kgo.RecordHeader{Key: k, Value: []byte(v)})
		}

		records = append(records, &kgo.Record{
			Topic:   topicTrending,
			Value:   data,
			Key:     []byte(market + ":" + t.id),
			Headers: headers,
		})
	}

	if len(records) == 0 {
		return nil
	}

	results := p.producer.ProduceSync(ctx, records...)
	for _, res := range results {
		if res.Err != nil {
			p.logger.Error("produce trending event", "err", res.Err)
		}
	}
	p.logger.Info("trending events published", "market", market, "count", len(records))
	return nil
}

type topicEntry struct {
	id    string
	name  string
	score float64
}

func (p *Publisher) fetchTopTopics(ctx context.Context, market string, n int) ([]topicEntry, error) {
	// Try Redis sorted set first (scores updated by grpc server)
	key := fmt.Sprintf("trending:%s", market)
	members, _ := p.rdb.ZRevRangeWithScores(ctx, key, 0, int64(n-1)).Result()
	if len(members) > 0 {
		var topics []topicEntry
		for _, m := range members {
			id, _ := m.Member.(string)
			// Look up name from DB
			name := id
			_ = p.db.QueryRow(ctx, `SELECT name FROM topics WHERE id::text = $1`, id).Scan(&name)
			topics = append(topics, topicEntry{id: id, name: name, score: m.Score})
		}
		return topics, nil
	}

	// Fallback: fresh query from DB
	rows, err := p.db.Query(ctx,
		`SELECT id::text, name, COALESCE(trend_score, 0.5)
		 FROM topics
		 WHERE (market = $1 OR market IS NULL)
		   AND id::text NOT IN (
		     SELECT topic_id FROM articles
		     WHERE market = $1 AND created_at > NOW() - INTERVAL '24 hours'
		   )
		 ORDER BY trend_score DESC NULLS LAST, created_at DESC NULLS LAST
		 LIMIT $2`,
		market, n,
	)
	if err != nil {
		return defaultTopicEntries(market, n), nil
	}
	defer rows.Close()

	var topics []topicEntry
	for rows.Next() {
		var e topicEntry
		if err := rows.Scan(&e.id, &e.name, &e.score); err == nil {
			topics = append(topics, e)
		}
	}
	if len(topics) == 0 {
		return defaultTopicEntries(market, n), nil
	}
	return topics, rows.Err()
}

func defaultTopicEntries(market string, n int) []topicEntry {
	all := map[string][]topicEntry{
		"italy": {
			{id: "seed-it-1", name: "Barolo 2019 vintage report", score: 0.9},
			{id: "seed-it-2", name: "Brunello di Montalcino trends", score: 0.8},
			{id: "seed-it-3", name: "Natural wine movement in Piedmont", score: 0.7},
		},
		"usa": {
			{id: "seed-us-1", name: "Napa Valley 2022 Cabernet season", score: 0.9},
			{id: "seed-us-2", name: "Oregon Pinot Noir rising producers", score: 0.8},
			{id: "seed-us-3", name: "Natural wine bars in NYC", score: 0.7},
		},
		"china": {
			{id: "seed-cn-1", name: "Premium Bordeaux gifting guide", score: 0.9},
			{id: "seed-cn-2", name: "LVMH wine portfolio 2024", score: 0.8},
			{id: "seed-cn-3", name: "Chinese domestic wine Ningxia", score: 0.7},
		},
	}
	entries := all[market]
	if n > len(entries) {
		n = len(entries)
	}
	return entries[:n]
}
