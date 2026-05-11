package grpcserver

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	analyticsv1 "github.com/newsroom/proto/analytics/v1"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var tracer = otel.Tracer("analytics/grpc")

type Server struct {
	analyticsv1.UnimplementedAnalyticsServiceServer
	db *pgxpool.Pool
}

func New(db *pgxpool.Pool) *Server {
	return &Server{db: db}
}

// RecordQualityScore stores an article quality score in analytics_svc.article_performance.
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

	_, err := s.db.Exec(ctx, `
		INSERT INTO article_performance (article_id, market, quality_score, created_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (article_id) DO UPDATE SET quality_score = EXCLUDED.quality_score
	`, req.ArticleId, req.Market, req.QualityScore)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "record quality: %v", err)
	}
	return &analyticsv1.QualityResponse{Recorded: true}, nil
}
