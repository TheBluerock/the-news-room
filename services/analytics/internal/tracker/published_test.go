package tracker

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
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
	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)

	// tracker writes to unqualified `article_performance` — create it in public schema.
	if _, err := pool.Exec(ctx, `
		CREATE TABLE article_performance (
			article_id    UUID PRIMARY KEY,
			market        TEXT NOT NULL,
			quality_score FLOAT NOT NULL DEFAULT 0,
			published_at  TIMESTAMPTZ,
			created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		t.Fatalf("schema: %v", err)
	}
	return pool
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestHandlePublished_InsertsRow(t *testing.T) {
	pool := startPG(t)
	ctx := context.Background()

	evt := publishedEvent{
		EventID:   "e1",
		ArticleID: "11111111-1111-1111-1111-111111111111",
		Market:    "italy",
		SanityID:  "sanity-abc",
		Timestamp: "2026-05-20T12:00:00Z",
	}
	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("marshal evt: %v", err)
	}

	if err := handlePublished(ctx, data, pool, quietLogger()); err != nil {
		t.Fatalf("handlePublished: %v", err)
	}

	var market string
	var publishedAt *string
	if err := pool.QueryRow(ctx,
		"SELECT market, published_at::text FROM article_performance WHERE article_id=$1::uuid",
		evt.ArticleID,
	).Scan(&market, &publishedAt); err != nil {
		t.Fatalf("query inserted row: %v", err)
	}
	if market != "italy" {
		t.Errorf("market = %q", market)
	}
	if publishedAt == nil {
		t.Error("published_at must be set")
	}
}

func TestHandlePublished_UpdateOnConflict(t *testing.T) {
	pool := startPG(t)
	ctx := context.Background()
	id := "22222222-2222-2222-2222-222222222222"

	// Pre-seed row with null published_at to verify ON CONFLICT path updates it.
	_, err := pool.Exec(ctx,
		`INSERT INTO article_performance (article_id, market, quality_score, published_at)
		 VALUES ($1::uuid, 'italy', 0.5, NULL)`, id)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	evt := publishedEvent{ArticleID: id, Market: "italy"}
	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("marshal evt: %v", err)
	}
	if err := handlePublished(ctx, data, pool, quietLogger()); err != nil {
		t.Fatalf("handlePublished: %v", err)
	}

	var publishedAt *string
	if err := pool.QueryRow(ctx,
		"SELECT published_at::text FROM article_performance WHERE article_id=$1::uuid", id,
	).Scan(&publishedAt); err != nil {
		t.Fatalf("query published_at: %v", err)
	}
	if publishedAt == nil {
		t.Error("published_at must be set after ON CONFLICT update")
	}
}

func TestHandlePublished_MalformedJSONReturnsError(t *testing.T) {
	pool := startPG(t)
	if err := handlePublished(context.Background(), []byte("{not-json"), pool, quietLogger()); err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestHandlePublished_DBErrorSwallowedNotReturned(t *testing.T) {
	pool := startPG(t)
	ctx := context.Background()

	// Drop the table so the INSERT fails — handlePublished logs but returns nil.
	_, _ = pool.Exec(ctx, `DROP TABLE article_performance`)
	evt := publishedEvent{ArticleID: "33333333-3333-3333-3333-333333333333", Market: "italy"}
	data, _ := json.Marshal(evt)
	if err := handlePublished(ctx, data, pool, quietLogger()); err != nil {
		t.Errorf("DB error must be swallowed (returns nil), got: %v", err)
	}
}
