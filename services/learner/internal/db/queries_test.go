package db

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	tcpg "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// startPG spins up Postgres with the pgvector extension preinstalled
// and applies the subset of learner_svc + analytics_svc schemas the queries touch.
func startPG(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	c, err := tcpg.Run(ctx,
		"pgvector/pgvector:pg16",
		tcpg.WithDatabase("learner_test"),
		tcpg.WithUsername("learner"),
		tcpg.WithPassword("learner"),
		tcpg.BasicWaitStrategies(),
		tcpg.WithSQLDriver("pgx"),
	)
	if err != nil {
		t.Skipf("pgvector container unavailable: %v", err)
	}
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })

	dsn, _ := c.ConnectionString(ctx, "sslmode=disable")
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)

	// Install pgvector extension BEFORE RegisterTypes can find the OID.
	if _, err := pool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS vector`); err != nil {
		t.Fatalf("create extension vector: %v", err)
	}
	// Drain the pool so the pgvector OID is discovered fresh by every connection.
	pool.Close()
	pool, err = pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool reopen: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := RegisterTypes(ctx, pool); err != nil {
		t.Fatalf("register pgvector: %v", err)
	}

	schema := []string{
		`CREATE EXTENSION IF NOT EXISTS pg_trgm`,
		`CREATE SCHEMA IF NOT EXISTS learner_svc`,
		`CREATE SCHEMA IF NOT EXISTS analytics_svc`,
		`CREATE TABLE learner_svc.journalist_profiles (
			journalist_id  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			market         TEXT NOT NULL,
			name           TEXT NOT NULL DEFAULT '',
			specialization TEXT,
			style_profile  JSONB,
			updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE learner_svc.sources (
			id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			market     TEXT NOT NULL,
			url        TEXT UNIQUE NOT NULL,
			title      TEXT,
			content    TEXT,
			embedding  VECTOR(1536),
			fetched_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX sources_fts_idx ON learner_svc.sources
			USING GIN (to_tsvector('english', coalesce(title,'') || ' ' || coalesce(content,'')))`,
		`CREATE TABLE learner_svc.article_embeddings (
			article_id UUID PRIMARY KEY,
			market     TEXT NOT NULL,
			embedding  VECTOR(1536),
			stale      BOOLEAN NOT NULL DEFAULT false,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE learner_svc.corrections (
			correction_id   TEXT PRIMARY KEY,
			market          TEXT NOT NULL,
			correction_type TEXT,
			reason          TEXT,
			old_value       TEXT,
			new_value       TEXT,
			applied_at      TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE learner_svc.rejections (
			article_id UUID PRIMARY KEY,
			market     TEXT NOT NULL,
			reason     TEXT,
			logged_at  TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE learner_svc.topic_suggestions (
			id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			market       TEXT NOT NULL,
			topic_id     TEXT NOT NULL,
			topic_name   TEXT NOT NULL,
			source_count INTEGER NOT NULL DEFAULT 1,
			example_urls TEXT[] NOT NULL DEFAULT '{}',
			suggested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE (market, topic_id)
		)`,
		`CREATE TABLE analytics_svc.article_performance (
			article_id    UUID PRIMARY KEY,
			market        TEXT NOT NULL,
			topic_id      TEXT,
			quality_score FLOAT NOT NULL DEFAULT 0,
			cultural_ok   BOOLEAN,
			factual_ok    BOOLEAN,
			published_at  TIMESTAMPTZ,
			created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
	}
	for _, s := range schema {
		if _, err := pool.Exec(ctx, s); err != nil {
			t.Fatalf("schema: %v\nSQL: %s", err, s)
		}
	}
	return pool
}

func mkVec(seed float32) []float32 {
	v := make([]float32, 1536)
	for i := range v {
		v[i] = seed + float32(i)/1536
	}
	return v
}

func TestUpsertSource_InsertThenUpdate(t *testing.T) {
	pool := startPG(t)
	ctx := context.Background()

	if err := UpsertSource(ctx, pool, "italy", "https://x.dev/a", "T1", "C1", mkVec(0.1)); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	// Second upsert with new content updates existing row (UNIQUE on url).
	if err := UpsertSource(ctx, pool, "italy", "https://x.dev/a", "T2", "C2 updated", mkVec(0.2)); err != nil {
		t.Fatalf("upsert update: %v", err)
	}

	var title, content string
	_ = pool.QueryRow(ctx, "SELECT title, content FROM learner_svc.sources WHERE url=$1", "https://x.dev/a").
		Scan(&title, &content)
	// Note: queries.go UPDATE does NOT update title — only content + embedding + fetched_at.
	if title != "T1" {
		t.Errorf("title = %q, want T1 (not updated by upsert path)", title)
	}
	if content != "C2 updated" {
		t.Errorf("content = %q", content)
	}
}

func TestUpsertEmbedding_StaleFlagReset(t *testing.T) {
	pool := startPG(t)
	ctx := context.Background()

	articleID := "11111111-1111-1111-1111-111111111111"
	_, _ = pool.Exec(ctx,
		`INSERT INTO learner_svc.article_embeddings (article_id, market, embedding, stale)
		 VALUES ($1::uuid, 'italy', NULL, true)`, articleID)

	if err := UpsertEmbedding(ctx, pool, articleID, "italy", mkVec(0.5)); err != nil {
		t.Fatalf("UpsertEmbedding: %v", err)
	}

	var stale bool
	_ = pool.QueryRow(ctx, "SELECT stale FROM learner_svc.article_embeddings WHERE article_id = $1::uuid", articleID).Scan(&stale)
	if stale {
		t.Error("stale must be cleared by UpsertEmbedding")
	}
}

func TestGetJournalistProfile_NotFound(t *testing.T) {
	pool := startPG(t)
	if _, err := GetJournalistProfile(context.Background(), pool, "00000000-0000-0000-0000-000000000000"); err == nil {
		t.Fatal("expected error on missing profile")
	}
}

func TestGetJournalistProfile_Found(t *testing.T) {
	pool := startPG(t)
	ctx := context.Background()
	id := "22222222-2222-2222-2222-222222222222"
	_, err := pool.Exec(ctx, `
		INSERT INTO learner_svc.journalist_profiles
		(journalist_id, market, name, specialization, style_profile)
		VALUES ($1::uuid, 'italy', 'Mario Rossi', 'natural wines', '{"tone":"formal"}'::jsonb)`, id)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	p, err := GetJournalistProfile(ctx, pool, id)
	if err != nil {
		t.Fatalf("GetJournalistProfile: %v", err)
	}
	if p.Market != "italy" || p.Specialization != "natural wines" {
		t.Errorf("profile = %+v", p)
	}
	if len(p.StyleProfile) == 0 {
		t.Error("style_profile bytes empty")
	}
}

func TestSearchSimilar_ReturnsResults(t *testing.T) {
	pool := startPG(t)
	ctx := context.Background()

	// Seed two article embeddings + one source.
	a1 := "33333333-3333-3333-3333-333333333333"
	a2 := "44444444-4444-4444-4444-444444444444"
	_ = UpsertEmbedding(ctx, pool, a1, "italy", mkVec(0.1))
	_ = UpsertEmbedding(ctx, pool, a2, "italy", mkVec(0.9))
	_ = UpsertSource(ctx, pool, "italy", "https://x.dev/s1", "Source", "Body", mkVec(0.1))

	// Quality score for a1 only — should weight it higher than a2 (no row, COALESCE default).
	_, _ = pool.Exec(ctx,
		`INSERT INTO analytics_svc.article_performance (article_id, market, quality_score) VALUES ($1::uuid, 'italy', 0.95)`, a1)

	results, err := SearchSimilar(ctx, pool, "italy", mkVec(0.1), 5)
	if err != nil {
		t.Fatalf("SearchSimilar: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected ≥1 result")
	}
	// Should contain both articles and the source.
	var hasArticle, hasSource bool
	for _, r := range results {
		if r.Type == "article" {
			hasArticle = true
		}
		if r.Type == "source" {
			hasSource = true
		}
	}
	if !hasArticle || !hasSource {
		t.Errorf("results missing types: %+v", results)
	}
}

func TestSearchSimilar_RespectsStaleFlag(t *testing.T) {
	pool := startPG(t)
	ctx := context.Background()

	a := "55555555-5555-5555-5555-555555555555"
	_ = UpsertEmbedding(ctx, pool, a, "italy", mkVec(0.1))
	_, _ = pool.Exec(ctx, "UPDATE learner_svc.article_embeddings SET stale = true WHERE article_id = $1::uuid", a)

	results, _ := SearchSimilar(ctx, pool, "italy", mkVec(0.1), 5)
	for _, r := range results {
		if r.Type == "article" && r.ID == a {
			t.Errorf("stale article should not appear in results")
		}
	}
}

func TestSearchSimilar_IsolatedPerMarket(t *testing.T) {
	pool := startPG(t)
	ctx := context.Background()

	_ = UpsertEmbedding(ctx, pool, "66666666-6666-6666-6666-666666666666", "italy", mkVec(0.1))
	results, _ := SearchSimilar(ctx, pool, "usa", mkVec(0.1), 5)
	for _, r := range results {
		if r.Type == "article" {
			t.Errorf("italy article leaked into usa search")
		}
	}
}

func TestSearchFullText(t *testing.T) {
	pool := startPG(t)
	ctx := context.Background()

	_ = UpsertSource(ctx, pool, "italy", "https://x.dev/a", "Barolo tasting", "Notes on Barolo 2018 vintage", mkVec(0.1))
	_ = UpsertSource(ctx, pool, "italy", "https://x.dev/b", "Pizza guide", "Roman style pizza", mkVec(0.1))

	results, err := SearchFullText(ctx, pool, "italy", "barolo", 10)
	if err != nil {
		t.Fatalf("FT search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1: %+v", len(results), results)
	}
	if results[0].Type != "source" {
		t.Errorf("type = %q", results[0].Type)
	}
}

func TestQualitySummary_DefaultsWhenEmpty(t *testing.T) {
	pool := startPG(t)
	s, err := GetMarketQualitySummary(context.Background(), pool, "italy")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if s.AvgQualityScore != 0.5 {
		t.Errorf("avg = %v, want 0.5 default", s.AvgQualityScore)
	}
	if s.ArticleCount30d != 0 {
		t.Errorf("count = %d, want 0", s.ArticleCount30d)
	}
}

func TestQualitySummary_AverageAndRejections(t *testing.T) {
	pool := startPG(t)
	ctx := context.Background()

	_, _ = pool.Exec(ctx, `
		INSERT INTO analytics_svc.article_performance (article_id, market, quality_score) VALUES
		(gen_random_uuid(), 'italy', 0.8),
		(gen_random_uuid(), 'italy', 0.6),
		(gen_random_uuid(), 'italy', 0.4)`)
	_, _ = pool.Exec(ctx, `
		INSERT INTO learner_svc.rejections (article_id, market, reason) VALUES
		(gen_random_uuid(), 'italy', 'tone'),
		(gen_random_uuid(), 'italy', 'tone'),
		(gen_random_uuid(), 'italy', 'factual')`)

	s, err := GetMarketQualitySummary(ctx, pool, "italy")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if s.AvgQualityScore < 0.59 || s.AvgQualityScore > 0.61 {
		t.Errorf("avg = %v, want ~0.6", s.AvgQualityScore)
	}
	if s.ArticleCount30d != 3 {
		t.Errorf("count = %d", s.ArticleCount30d)
	}
	if len(s.TopRejections) == 0 || s.TopRejections[0] != "tone" {
		t.Errorf("top rejections = %v", s.TopRejections)
	}
}

func TestLogCorrection_Idempotent(t *testing.T) {
	pool := startPG(t)
	ctx := context.Background()
	if err := LogCorrection(ctx, pool, "c1", "italy", "tone", "use formal", "old", "new"); err != nil {
		t.Fatalf("first: %v", err)
	}
	// Same correction_id with different reason — should NOT overwrite.
	if err := LogCorrection(ctx, pool, "c1", "italy", "tone", "different", "x", "y"); err != nil {
		t.Fatalf("second: %v", err)
	}
	var reason string
	_ = pool.QueryRow(ctx, "SELECT reason FROM learner_svc.corrections WHERE correction_id=$1", "c1").Scan(&reason)
	if reason != "use formal" {
		t.Errorf("reason = %q (idempotency violated)", reason)
	}
}

func TestLogRejection_UpsertsReason(t *testing.T) {
	pool := startPG(t)
	ctx := context.Background()
	a := "77777777-7777-7777-7777-777777777777"
	if err := LogRejection(ctx, pool, a, "italy", "first reason"); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := LogRejection(ctx, pool, a, "italy", "second reason"); err != nil {
		t.Fatalf("second: %v", err)
	}
	var reason string
	_ = pool.QueryRow(ctx, "SELECT reason FROM learner_svc.rejections WHERE article_id=$1::uuid", a).Scan(&reason)
	if reason != "second reason" {
		t.Errorf("reason = %q (expected upsert overwrite)", reason)
	}
}

func TestSourceExists_AbsentIsFalseNilErr(t *testing.T) {
	pool := startPG(t)
	exists, err := SourceExists(context.Background(), pool, "https://nope.dev/x", time.Hour)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if exists {
		t.Error("absent URL should not exist")
	}
}

func TestSourceExists_FreshlyScraped(t *testing.T) {
	pool := startPG(t)
	ctx := context.Background()
	_ = UpsertSource(ctx, pool, "italy", "https://x.dev/recent", "T", "C", mkVec(0.1))
	ok, err := SourceExists(ctx, pool, "https://x.dev/recent", time.Hour)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !ok {
		t.Error("recently scraped URL should exist within window")
	}
}

func TestSourceExists_StaleReturnsFalse(t *testing.T) {
	pool := startPG(t)
	ctx := context.Background()
	_ = UpsertSource(ctx, pool, "italy", "https://x.dev/old", "T", "C", mkVec(0.1))
	// Rewind fetched_at past the maxAge window.
	_, _ = pool.Exec(ctx, "UPDATE learner_svc.sources SET fetched_at = now() - INTERVAL '2 hours' WHERE url=$1", "https://x.dev/old")
	ok, _ := SourceExists(ctx, pool, "https://x.dev/old", time.Hour)
	if ok {
		t.Error("stale URL should be considered missing")
	}
}

func TestUpsertTopicSuggestion_ThenGet(t *testing.T) {
	pool := startPG(t)
	ctx := context.Background()
	s := TopicSuggestionRow{TopicID: "barolo", TopicName: "Barolo 2018", SourceCount: 5, ExampleURLs: []string{"u1", "u2"}}
	if err := UpsertTopicSuggestion(ctx, pool, "italy", s); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	// Update with new count.
	s.SourceCount = 7
	if err := UpsertTopicSuggestion(ctx, pool, "italy", s); err != nil {
		t.Fatalf("upsert 2: %v", err)
	}

	results, err := GetTopicSuggestions(ctx, pool, "italy", 10)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d, want 1: %+v", len(results), results)
	}
	if results[0].SourceCount != 7 {
		t.Errorf("source count = %d, want 7", results[0].SourceCount)
	}
}

func TestRecentSourceTitles(t *testing.T) {
	pool := startPG(t)
	ctx := context.Background()
	_ = UpsertSource(ctx, pool, "italy", "https://a/1", "Barolo notes", "x", mkVec(0.1))
	_ = UpsertSource(ctx, pool, "italy", "https://a/2", "Nebbiolo", "x", mkVec(0.1))
	// Untitled source — should be excluded.
	_, _ = pool.Exec(ctx, `INSERT INTO learner_svc.sources (market, url, title) VALUES ('italy', 'https://a/3', '')`)

	titles, urls, err := RecentSourceTitles(ctx, pool, "italy", 7, 10)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(titles) != 2 || len(urls) != 2 {
		t.Errorf("got %d titles, %d urls", len(titles), len(urls))
	}
}
