package grpcserver

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	analyticsv1 "github.com/newsroom/proto/analytics/v1"
	tcpg "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func startPG(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	c, err := tcpg.Run(ctx,
		"postgres:16-alpine",
		tcpg.WithDatabase("analytics_test"),
		tcpg.WithUsername("analytics"),
		tcpg.WithPassword("analytics"),
		tcpg.BasicWaitStrategies(),
		tcpg.WithSQLDriver("pgx"),
	)
	if err != nil {
		t.Skipf("pg container unavailable: %v", err)
	}
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })
	dsn, _ := c.ConnectionString(ctx, "sslmode=disable")
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)

	// RecordQualityScore writes to unqualified `article_performance` (cross-schema
	// drift, tracked in audit Section A). For tests we use the public schema.
	if _, err := pool.Exec(ctx, `
		CREATE TABLE article_performance (
			article_id            UUID PRIMARY KEY,
			market                TEXT NOT NULL,
			quality_score         FLOAT NOT NULL DEFAULT 0,
			prompt_tokens         INTEGER,
			completion_tokens     INTEGER,
			model                 TEXT,
			cost_usd              NUMERIC(10, 6),
			quality_per_1k_tokens DOUBLE PRECISION,
			published_at          TIMESTAMPTZ,
			created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		t.Fatalf("schema: %v", err)
	}
	return pool
}

func TestRecordQualityScore_RejectsMissingArticleId(t *testing.T) {
	pool := startPG(t)
	s := New(pool)
	_, err := s.RecordQualityScore(context.Background(), &analyticsv1.QualityRequest{
		Market: "italy",
	})
	if err == nil {
		t.Fatal("expected error on missing article_id")
	}
}

func TestRecordQualityScore_PersistsBasicFields(t *testing.T) {
	pool := startPG(t)
	s := New(pool)
	ctx := context.Background()
	id := "11111111-1111-1111-1111-111111111111"

	_, err := s.RecordQualityScore(ctx, &analyticsv1.QualityRequest{
		ArticleId:    id,
		Market:       "italy",
		QualityScore: 0.87,
	})
	if err != nil {
		t.Fatalf("RecordQualityScore: %v", err)
	}

	var qs float64
	var promptT, completionT *int
	var model *string
	var costUSD *float64
	var qPer1k *float64
	row := pool.QueryRow(ctx, `
		SELECT quality_score, prompt_tokens, completion_tokens, model, cost_usd::float8, quality_per_1k_tokens
		FROM article_performance WHERE article_id=$1::uuid`, id,
	)
	if err := row.Scan(&qs, &promptT, &completionT, &model, &costUSD, &qPer1k); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if qs < 0.86 || qs > 0.88 {
		t.Errorf("quality_score = %v", qs)
	}
	if promptT != nil || completionT != nil || model != nil || costUSD != nil || qPer1k != nil {
		t.Errorf("token/cost columns should be NULL when usage not provided; got "+
			"prompt=%v completion=%v model=%v cost=%v q/1k=%v",
			promptT, completionT, model, costUSD, qPer1k)
	}
}

func TestRecordQualityScore_PersistsCostFields(t *testing.T) {
	pool := startPG(t)
	s := New(pool)
	ctx := context.Background()
	id := "22222222-2222-2222-2222-222222222222"

	_, err := s.RecordQualityScore(ctx, &analyticsv1.QualityRequest{
		ArticleId:        id,
		Market:           "italy",
		QualityScore:     0.80,
		PromptTokens:     1000,
		CompletionTokens: 500,
		Model:            "gpt-4o",
	})
	if err != nil {
		t.Fatalf("RecordQualityScore: %v", err)
	}

	var promptT, completionT int
	var model string
	var costUSD, qPer1k float64
	err = pool.QueryRow(ctx, `
		SELECT prompt_tokens, completion_tokens, model, cost_usd::float8, quality_per_1k_tokens
		FROM article_performance WHERE article_id=$1::uuid`, id,
	).Scan(&promptT, &completionT, &model, &costUSD, &qPer1k)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if promptT != 1000 || completionT != 500 || model != "gpt-4o" {
		t.Errorf("usage cols: prompt=%d completion=%d model=%q", promptT, completionT, model)
	}
	// Expected: (1000/1M * 2.50) + (500/1M * 10.00) = 0.0075
	if costUSD < 0.0074 || costUSD > 0.0076 {
		t.Errorf("cost_usd = %v, want ~0.0075", costUSD)
	}
	// quality_per_1k = quality / (total_tokens / 1000) = 0.80 / 1.5 ≈ 0.533
	if qPer1k < 0.52 || qPer1k > 0.55 {
		t.Errorf("quality_per_1k = %v, want ~0.533", qPer1k)
	}
}

func TestRecordQualityScore_OnConflictPreservesCostWhenNotProvided(t *testing.T) {
	pool := startPG(t)
	s := New(pool)
	ctx := context.Background()
	id := "33333333-3333-3333-3333-333333333333"

	// First insert with usage.
	_, _ = s.RecordQualityScore(ctx, &analyticsv1.QualityRequest{
		ArticleId: id, Market: "italy", QualityScore: 0.7,
		PromptTokens: 1000, CompletionTokens: 500, Model: "gpt-4o",
	})

	// Second insert without usage — must NOT wipe cost cols.
	_, _ = s.RecordQualityScore(ctx, &analyticsv1.QualityRequest{
		ArticleId: id, Market: "italy", QualityScore: 0.9,
	})

	var qs float64
	var model string
	_ = pool.QueryRow(ctx, `SELECT quality_score, model FROM article_performance WHERE article_id=$1::uuid`, id).
		Scan(&qs, &model)
	if qs < 0.89 || qs > 0.91 {
		t.Errorf("quality_score not updated: %v", qs)
	}
	if model != "gpt-4o" {
		t.Errorf("model column was wiped by second call: %q", model)
	}
}
