package trends

import (
	"context"
	"io"
	"log/slog"
	"strconv"
	"testing"
	"time"

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

	// Code references unqualified `editorial_calendar` — create in public schema.
	if _, err := pool.Exec(ctx, `
		CREATE TABLE editorial_calendar (
			id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			market        TEXT NOT NULL,
			topic_id      TEXT NOT NULL,
			topic_name    TEXT NOT NULL,
			scheduled_at  TIMESTAMPTZ NOT NULL,
			dispatched    BOOLEAN NOT NULL DEFAULT false,
			dispatched_at TIMESTAMPTZ
		)`); err != nil {
		t.Fatalf("schema: %v", err)
	}
	return pool
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

func mkPub(pool *pgxpool.Pool) *Publisher {
	return &Publisher{db: pool, producer: nil, interval: time.Hour, logger: quietLogger()}
}

func TestFetchDue_Empty(t *testing.T) {
	pool := startPG(t)
	p := mkPub(pool)
	got, err := p.fetchDue(context.Background())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d, want 0", len(got))
	}
}

func TestFetchDue_OnlyDueAndUndispatched(t *testing.T) {
	pool := startPG(t)
	ctx := context.Background()

	_, err := pool.Exec(ctx, `
		INSERT INTO editorial_calendar (market, topic_id, topic_name, scheduled_at, dispatched) VALUES
		('italy', 'past-1',     'Past topic', now() - interval '1 hour', false),
		('usa',   'past-2',     'USA past',   now() - interval '2 hour', false),
		('italy', 'done',       'Already',    now() - interval '3 hour', true),
		('italy', 'future',     'Future',     now() + interval '1 hour', false)
	`)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	p := mkPub(pool)
	got, err := p.fetchDue(ctx)
	if err != nil {
		t.Fatalf("fetchDue: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d, want 2 (past + undispatched)", len(got))
	}

	// Order: scheduled_at ASC → past-2 (2h ago) before past-1 (1h ago).
	if got[0].topicID != "past-2" {
		t.Errorf("order broken: got[0]=%q, want past-2", got[0].topicID)
	}
	if got[1].topicID != "past-1" {
		t.Errorf("order broken: got[1]=%q, want past-1", got[1].topicID)
	}
}

func TestFetchDue_LimitedTo50(t *testing.T) {
	pool := startPG(t)
	ctx := context.Background()

	// Insert 60 past, undispatched rows.
	for i := 0; i < 60; i++ {
		_, err := pool.Exec(ctx, `
			INSERT INTO editorial_calendar (market, topic_id, topic_name, scheduled_at, dispatched)
			VALUES ($1, $2, $3, now() - make_interval(mins => $4), false)`,
			"italy", "topic-"+strconv.Itoa(i), "Topic", i+1)
		if err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	p := mkPub(pool)
	got, err := p.fetchDue(ctx)
	if err != nil {
		t.Fatalf("fetchDue: %v", err)
	}
	if len(got) != 50 {
		t.Errorf("got %d, want 50 (LIMIT)", len(got))
	}
}

func TestMarkDispatched_SetsFlagAndTimestamp(t *testing.T) {
	pool := startPG(t)
	ctx := context.Background()

	var id string
	if err := pool.QueryRow(ctx, `
		INSERT INTO editorial_calendar (market, topic_id, topic_name, scheduled_at, dispatched)
		VALUES ('italy', 't1', 'Topic', now() - interval '1 hour', false) RETURNING id::text
	`).Scan(&id); err != nil {
		t.Fatalf("seed insert: %v", err)
	}

	p := mkPub(pool)
	if err := p.markDispatched(ctx, id); err != nil {
		t.Fatalf("markDispatched: %v", err)
	}

	var dispatched bool
	var dispatchedAt *string
	if err := pool.QueryRow(ctx, `
		SELECT dispatched, dispatched_at::text
		FROM editorial_calendar WHERE id::text = $1
	`, id).Scan(&dispatched, &dispatchedAt); err != nil {
		t.Fatalf("query dispatched: %v", err)
	}
	if !dispatched {
		t.Error("dispatched flag not set")
	}
	if dispatchedAt == nil {
		t.Error("dispatched_at not set")
	}
}

func TestMarkDispatched_AbsentIDNoError(t *testing.T) {
	pool := startPG(t)
	p := mkPub(pool)
	if err := p.markDispatched(context.Background(), "00000000-0000-0000-0000-000000000000"); err != nil {
		t.Errorf("absent id should be no-op, got error: %v", err)
	}
}

func TestNewPublisher_BadBrokersStillBuildsClient(t *testing.T) {
	// kgo.NewClient with seed brokers does NOT dial — it lazily connects.
	// So we can verify NewPublisher path is exercised even with garbage brokers.
	pool := startPG(t)
	p, err := NewPublisher(pool, []string{"127.0.0.1:65530"}, time.Second, quietLogger())
	if err != nil {
		t.Fatalf("NewPublisher: %v", err)
	}
	if p == nil {
		t.Fatal("nil publisher")
	}
	p.Close()
}
