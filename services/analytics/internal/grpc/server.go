package grpcserver

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	analyticsv1 "github.com/newsroom/proto/analytics/v1"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var tracer = otel.Tracer("analytics/grpc")

type Server struct {
	analyticsv1.UnimplementedAnalyticsServiceServer
	db  *pgxpool.Pool
	rdb *redis.Client
}

func New(db *pgxpool.Pool, rdb *redis.Client) *Server {
	return &Server{db: db, rdb: rdb}
}

// GetTrendingSignals returns top trending topics for a market from Redis cache.
// Falls back to PostgreSQL if cache is cold.
func (s *Server) GetTrendingSignals(ctx context.Context, req *analyticsv1.TrendingRequest) (*analyticsv1.TrendingResponse, error) {
	ctx, span := tracer.Start(ctx, "GetTrendingSignals")
	defer span.End()

	if req.Market == "" {
		return nil, status.Error(codes.InvalidArgument, "market is required")
	}
	limit := req.Limit
	if limit <= 0 || limit > 20 {
		limit = 5
	}

	span.SetAttributes(attribute.String("market", req.Market), attribute.Int("limit", int(limit)))

	topics, err := s.fetchTrendingFromCache(ctx, req.Market, int(limit))
	if err != nil || len(topics) == 0 {
		topics, err = s.fetchTrendingFromDB(ctx, req.Market, int(limit))
		if err != nil {
			return nil, status.Errorf(codes.Internal, "trending fetch failed: %v", err)
		}
	}

	return &analyticsv1.TrendingResponse{Topics: topics}, nil
}

// RecordQualityScore stores an article quality score in PostgreSQL.
func (s *Server) RecordQualityScore(ctx context.Context, req *analyticsv1.QualityRequest) (*analyticsv1.QualityResponse, error) {
	ctx, span := tracer.Start(ctx, "RecordQualityScore")
	defer span.End()

	if req.ArticleId == "" || req.Market == "" {
		return nil, status.Error(codes.InvalidArgument, "article_id and market are required")
	}
	span.SetAttributes(
		attribute.String("article_id", req.ArticleId),
		attribute.String("market", req.Market),
		attribute.Float64("quality_score", float64(req.QualityScore)),
	)

	_, err := s.db.Exec(ctx,
		`INSERT INTO performance (article_id, market, quality_score, recorded_at)
		 VALUES ($1, $2, $3, NOW())
		 ON CONFLICT (article_id) DO UPDATE SET quality_score = $3, recorded_at = NOW()`,
		req.ArticleId, req.Market, req.QualityScore,
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "record quality: %v", err)
	}
	return &analyticsv1.QualityResponse{Recorded: true}, nil
}

func (s *Server) fetchTrendingFromCache(ctx context.Context, market string, limit int) ([]*analyticsv1.TrendingTopic, error) {
	key := fmt.Sprintf("trending:%s", market)
	members, err := s.rdb.ZRevRangeWithScores(ctx, key, 0, int64(limit-1)).Result()
	if err != nil || len(members) == 0 {
		return nil, err
	}
	var topics []*analyticsv1.TrendingTopic
	for _, m := range members {
		topics = append(topics, &analyticsv1.TrendingTopic{
			TopicId:    m.Member.(string),
			TopicName:  m.Member.(string),
			TrendScore: float32(m.Score),
		})
	}
	return topics, nil
}

func (s *Server) fetchTrendingFromDB(ctx context.Context, market string, limit int) ([]*analyticsv1.TrendingTopic, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id::text, name, COALESCE(trend_score, 0.5)
		 FROM topics
		 WHERE market = $1 OR market IS NULL
		 ORDER BY updated_at DESC NULLS LAST
		 LIMIT $2`,
		market, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("db query: %w", err)
	}
	defer rows.Close()

	var topics []*analyticsv1.TrendingTopic
	for rows.Next() {
		var t analyticsv1.TrendingTopic
		if err := rows.Scan(&t.TopicId, &t.TopicName, &t.TrendScore); err != nil {
			continue
		}
		topics = append(topics, &t)
	}
	// Seed default topics if DB is empty (initial dev state)
	if len(topics) == 0 {
		topics = defaultTopics(market)
	}
	return topics, rows.Err()
}

func defaultTopics(market string) []*analyticsv1.TrendingTopic {
	switch market {
	case "italy":
		return []*analyticsv1.TrendingTopic{
			{TopicId: "seed-it-1", TopicName: "Barolo 2019 vintage report", TrendScore: 0.9},
			{TopicId: "seed-it-2", TopicName: "Brunello di Montalcino trends", TrendScore: 0.8},
			{TopicId: "seed-it-3", TopicName: "Natural wine movement in Piedmont", TrendScore: 0.7},
		}
	case "usa":
		return []*analyticsv1.TrendingTopic{
			{TopicId: "seed-us-1", TopicName: "Napa Valley 2022 Cabernet season", TrendScore: 0.9},
			{TopicId: "seed-us-2", TopicName: "Oregon Pinot Noir rising producers", TrendScore: 0.8},
			{TopicId: "seed-us-3", TopicName: "Natural wine bars in NYC", TrendScore: 0.7},
		}
	default: // china
		return []*analyticsv1.TrendingTopic{
			{TopicId: "seed-cn-1", TopicName: "Premium Bordeaux gifting guide", TrendScore: 0.9},
			{TopicId: "seed-cn-2", TopicName: "LVMH wine portfolio 2024", TrendScore: 0.8},
			{TopicId: "seed-cn-3", TopicName: "Chinese domestic wine Ningxia", TrendScore: 0.7},
		}
	}
}

// UpdateTrendingCache refreshes the Redis sorted set for a market from DB scores.
func (s *Server) UpdateTrendingCache(ctx context.Context, market, topicID, topicName string, score float64) error {
	key := fmt.Sprintf("trending:%s", market)
	return s.rdb.ZAdd(ctx, key, redis.Z{Score: score, Member: topicID}).Err()
}

// RecordPublishedArticle increments the publish count for a topic in the trending cache.
func (s *Server) RecordPublishedArticle(ctx context.Context, market, topicID string) error {
	key := fmt.Sprintf("trending:%s", market)
	// Published articles reduce trend score (topic is now "covered")
	_ = s.rdb.ZIncrBy(ctx, key, -0.1, topicID)
	// Also store publish timestamp to avoid re-covering same topic too soon
	coverKey := fmt.Sprintf("covered:%s:%s", market, topicID)
	return s.rdb.Set(ctx, coverKey, time.Now().UTC().Format(time.RFC3339), 7*24*time.Hour).Err()
}
