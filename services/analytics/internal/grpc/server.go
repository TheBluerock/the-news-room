package grpcserver

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	analyticsv1 "github.com/newsroom/proto/analytics/v1"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var tracer = otel.Tracer("analytics/grpc")

// Prometheus metrics for LLM spend tracking. Mirrors agent-side metrics:
// the agent counts each call locally; analytics counts cost as it's persisted
// (authoritative ledger view).
var (
	llmTokensTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "llm_tokens_total",
		Help: "Total LLM tokens consumed (analytics persistence view).",
	}, []string{"market", "model", "type"})

	llmCostUSDTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "llm_cost_usd_total",
		Help: "Cumulative USD spend on LLM API calls (analytics persistence view).",
	}, []string{"market", "model"})
)

type Server struct {
	analyticsv1.UnimplementedAnalyticsServiceServer
	db *pgxpool.Pool
}

func New(db *pgxpool.Pool) *Server {
	return &Server{db: db}
}

// RecordQualityScore stores an article quality score, token usage, and
// computed USD cost in analytics_svc.article_performance.
//
// Token fields (prompt_tokens, completion_tokens, model) are optional —
// callers that lack token data may omit them; cost will be NULL and quality
// efficiency will be NULL for that row.
func (s *Server) RecordQualityScore(ctx context.Context, req *analyticsv1.QualityRequest) (*analyticsv1.QualityResponse, error) {
	ctx, span := tracer.Start(ctx, "RecordQualityScore")
	defer span.End()

	if req.ArticleId == "" || req.Market == "" {
		return nil, status.Error(codes.InvalidArgument, "article_id and market are required")
	}

	hasUsage := req.Model != "" && (req.PromptTokens+req.CompletionTokens) > 0
	cost := computeCostUSD(req.Model, req.PromptTokens, req.CompletionTokens)

	totalTokens := req.PromptTokens + req.CompletionTokens
	var qualityPer1k any = nil
	if hasUsage && totalTokens > 0 {
		qualityPer1k = float64(req.QualityScore) / (float64(totalTokens) / 1000.0)
	}

	var (
		promptArg     any = nil
		completionArg any = nil
		modelArg      any = nil
		costArg       any = nil
	)
	if hasUsage {
		promptArg = req.PromptTokens
		completionArg = req.CompletionTokens
		modelArg = req.Model
		costArg = cost
	}

	span.SetAttributes(
		attribute.String("article_id", req.ArticleId),
		attribute.String("market", req.Market),
		attribute.Float64("quality_score", float64(req.QualityScore)),
		attribute.Int64("prompt_tokens", int64(req.PromptTokens)),
		attribute.Int64("completion_tokens", int64(req.CompletionTokens)),
		attribute.String("model", req.Model),
		attribute.Float64("cost_usd", cost),
	)

	_, err := s.db.Exec(ctx, `
		INSERT INTO article_performance
			(article_id, market, quality_score,
			 prompt_tokens, completion_tokens, model,
			 cost_usd, quality_per_1k_tokens, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now())
		ON CONFLICT (article_id) DO UPDATE SET
			quality_score         = EXCLUDED.quality_score,
			prompt_tokens         = COALESCE(EXCLUDED.prompt_tokens, article_performance.prompt_tokens),
			completion_tokens     = COALESCE(EXCLUDED.completion_tokens, article_performance.completion_tokens),
			model                 = COALESCE(EXCLUDED.model, article_performance.model),
			cost_usd              = COALESCE(EXCLUDED.cost_usd, article_performance.cost_usd),
			quality_per_1k_tokens = COALESCE(EXCLUDED.quality_per_1k_tokens, article_performance.quality_per_1k_tokens)
	`,
		req.ArticleId, req.Market, req.QualityScore,
		promptArg, completionArg, modelArg,
		costArg, qualityPer1k,
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "record quality: %v", err)
	}

	if hasUsage {
		llmTokensTotal.WithLabelValues(req.Market, req.Model, "prompt").Add(float64(req.PromptTokens))
		llmTokensTotal.WithLabelValues(req.Market, req.Model, "completion").Add(float64(req.CompletionTokens))
		llmCostUSDTotal.WithLabelValues(req.Market, req.Model).Add(cost)
	}

	return &analyticsv1.QualityResponse{Recorded: true}, nil
}
