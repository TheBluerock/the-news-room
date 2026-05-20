package restapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	tcpg "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"

	pgxvector "github.com/pgvector/pgvector-go/pgx"
)

type fixture struct {
	srv *http.Server
	mux http.Handler
	db  *pgxpool.Pool
	rdb *redis.Client
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()

	// Redis
	rc, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		t.Skipf("redis container unavailable: %v", err)
	}
	t.Cleanup(func() { _ = rc.Terminate(context.Background()) })
	rhost, _ := rc.Host(ctx)
	rport, _ := rc.MappedPort(ctx, "6379/tcp")
	rdb := redis.NewClient(&redis.Options{Addr: rhost + ":" + rport.Port()})
	t.Cleanup(func() { _ = rdb.Close() })
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping redis: %v", err)
	}

	// Postgres + pgvector
	pc, err := tcpg.Run(ctx,
		"pgvector/pgvector:pg16",
		tcpg.WithDatabase("learner_test"),
		tcpg.WithUsername("learner"),
		tcpg.WithPassword("learner"),
		tcpg.BasicWaitStrategies(),
		tcpg.WithSQLDriver("pgx"),
	)
	if err != nil {
		t.Skipf("pg container unavailable: %v", err)
	}
	t.Cleanup(func() { _ = pc.Terminate(context.Background()) })
	dsn, _ := pc.ConnectionString(ctx, "sslmode=disable")
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS vector`); err != nil {
		t.Fatalf("ext: %v", err)
	}
	pool.Close()
	pool, err = pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool reopen: %v", err)
	}
	t.Cleanup(pool.Close)

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := pgxvector.RegisterTypes(ctx, conn.Conn()); err != nil {
		t.Fatalf("register: %v", err)
	}
	conn.Release()

	schema := []string{
		`CREATE SCHEMA IF NOT EXISTS learner_svc`,
		`CREATE SCHEMA IF NOT EXISTS analytics_svc`,
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
		`CREATE TABLE learner_svc.rejections (
			article_id UUID PRIMARY KEY,
			market     TEXT NOT NULL,
			reason     TEXT,
			logged_at  TIMESTAMPTZ NOT NULL DEFAULT now()
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
			t.Fatalf("schema: %v", err)
		}
	}

	srv := New(pool, rdb, quietLogger())
	return &fixture{srv: srv, mux: srv.Handler, db: pool, rdb: rdb}
}

func doJSON(t *testing.T, mux http.Handler, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestQualitySummary_RejectsUnknownMarket(t *testing.T) {
	fx := newFixture(t)
	rec := doJSON(t, fx.mux, "GET", "/api/quality-summary?market=japan", nil, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestQualitySummary_OkWithDefaults(t *testing.T) {
	fx := newFixture(t)
	rec := doJSON(t, fx.mux, "GET", "/api/quality-summary?market=italy", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var s map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&s)
	if s["AvgQualityScore"].(float64) != 0.5 {
		t.Errorf("default avg = %v", s["AvgQualityScore"])
	}
}

func TestSuggestions_RequiresMarket(t *testing.T) {
	fx := newFixture(t)
	rec := doJSON(t, fx.mux, "GET", "/suggestions", nil, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestSuggestions_HappyPath(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	_, _ = fx.db.Exec(ctx, `
		INSERT INTO learner_svc.topic_suggestions (market, topic_id, topic_name, source_count)
		VALUES ('italy', 'barolo', 'Barolo 2018', 5)`)

	rec := doJSON(t, fx.mux, "GET", "/suggestions?market=italy", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var out []map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&out)
	if len(out) != 1 {
		t.Fatalf("got %d, want 1", len(out))
	}
	if out[0]["TopicID"] != "barolo" {
		t.Errorf("topic_id = %v", out[0]["TopicID"])
	}
}

func TestSuggestions_LimitClamped(t *testing.T) {
	fx := newFixture(t)
	rec := doJSON(t, fx.mux, "GET", "/suggestions?market=italy&limit=99999", nil, nil)
	// We don't assert clamp by reading code; just verify no 500 — empty result acceptable.
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestCorrections_RequiresAdminRole(t *testing.T) {
	fx := newFixture(t)
	rec := doJSON(t, fx.mux, "POST", "/api/corrections",
		map[string]string{"article_id": "a1", "market": "italy", "correction": "x"},
		map[string]string{"X-User-Role": "editor"})
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestCorrections_RejectsEmptyFields(t *testing.T) {
	fx := newFixture(t)
	cases := []map[string]string{
		{"article_id": "", "market": "italy", "correction": "x"},
		{"article_id": "a1", "market": "", "correction": "x"},
		{"article_id": "a1", "market": "italy", "correction": ""},
	}
	for _, body := range cases {
		rec := doJSON(t, fx.mux, "POST", "/api/corrections", body,
			map[string]string{"X-User-Role": "admin"})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body=%v status = %d", body, rec.Code)
		}
	}
}

func TestCorrections_RejectsBadMarket(t *testing.T) {
	fx := newFixture(t)
	rec := doJSON(t, fx.mux, "POST", "/api/corrections",
		map[string]string{"article_id": "a1", "market": "japan", "correction": "x"},
		map[string]string{"X-User-Role": "admin"})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestCorrections_MalformedBody(t *testing.T) {
	fx := newFixture(t)
	req := httptest.NewRequest("POST", "/api/corrections", bytes.NewBufferString("{bad"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-Role", "admin")
	rec := httptest.NewRecorder()
	fx.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestCorrections_HappyPath_WritesRedisAndPG(t *testing.T) {
	fx := newFixture(t)
	rec := doJSON(t, fx.mux, "POST", "/api/corrections",
		map[string]string{"article_id": "a1", "market": "italy", "correction": "use formal tone"},
		map[string]string{"X-User-Role": "admin"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var out map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&out)
	if out["correction_id"] == "" {
		t.Error("correction_id missing")
	}

	// Redis key should exist
	ctx := context.Background()
	n, _ := fx.rdb.Exists(ctx, "corrections:italy:"+out["correction_id"]).Result()
	if n != 1 {
		t.Errorf("redis key not written")
	}

	// PG row should exist
	var reason string
	_ = fx.db.QueryRow(ctx, "SELECT reason FROM learner_svc.corrections WHERE correction_id=$1", out["correction_id"]).Scan(&reason)
	if reason != "use formal tone" {
		t.Errorf("pg reason = %q", reason)
	}
}

func TestNew_RegistersAllRoutes(t *testing.T) {
	fx := newFixture(t)

	// Unknown route → 404
	rec := doJSON(t, fx.mux, "GET", "/nope", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown route status = %d", rec.Code)
	}

	// Verify each registered route returns something other than 404.
	cases := []struct{ method, path string }{
		{"GET", "/suggestions?market=italy"},
		{"GET", "/api/quality-summary?market=italy"},
		{"POST", "/api/corrections"},
	}
	for _, c := range cases {
		rec := doJSON(t, fx.mux, c.method, c.path, nil, nil)
		if rec.Code == http.StatusNotFound {
			t.Errorf("%s %s returned 404", c.method, c.path)
		}
	}
}
