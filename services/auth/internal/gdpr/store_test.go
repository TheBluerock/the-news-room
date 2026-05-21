package gdpr

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	tcpg "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// startPG spins up postgres + applies just enough schema (auth_svc.users,
// user_deletions, public.audit_log) for the GDPR store to exercise.
func startPG(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	c, err := tcpg.Run(ctx,
		"postgres:16-alpine",
		tcpg.WithDatabase("auth_test"),
		tcpg.WithUsername("auth"),
		tcpg.WithPassword("auth"),
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

	stmts := []string{
		`CREATE EXTENSION IF NOT EXISTS pgcrypto`,
		// Mirror migration 001 + 002 + 010: public.users with deleted_at.
		`CREATE TABLE users (
			id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			email         TEXT UNIQUE NOT NULL,
			market        TEXT,
			password_hash TEXT NOT NULL DEFAULT '',
			created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
			deleted_at    TIMESTAMPTZ
		)`,
		`CREATE TABLE user_deletions (
			user_id             UUID PRIMARY KEY,
			requested_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
			requested_by        UUID NOT NULL,
			completed_at        TIMESTAMPTZ,
			services_completed  JSONB NOT NULL DEFAULT '{}'::jsonb
		)`,
		`CREATE TABLE audit_log (
			id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			timestamp   TIMESTAMPTZ NOT NULL DEFAULT now(),
			user_id     UUID NOT NULL,
			action      TEXT NOT NULL,
			resource_id UUID NOT NULL DEFAULT gen_random_uuid()
		)`,
		// Seed sentinel user.
		`INSERT INTO users (id, email) VALUES ('` + AnonymisedSentinelID + `', 'anonymised@deleted.local')`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(ctx, s); err != nil {
			t.Fatalf("schema: %v\nSQL: %s", err, s)
		}
	}
	return pool
}

func seedUser(t *testing.T, pool *pgxpool.Pool, email string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO users (email) VALUES ($1) RETURNING id::text`, email,
	).Scan(&id); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return id
}

func TestRecordRequest_FirstCallReturnsTimestamp(t *testing.T) {
	pool := startPG(t)
	uid := seedUser(t, pool, "alice@x.dev")

	ts, err := RecordRequest(context.Background(), pool, uid, uid)
	if err != nil {
		t.Fatalf("RecordRequest: %v", err)
	}
	if ts.IsZero() {
		t.Fatal("requested_at must be populated")
	}
}

func TestRecordRequest_IsIdempotent(t *testing.T) {
	pool := startPG(t)
	uid := seedUser(t, pool, "alice@x.dev")

	first, err := RecordRequest(context.Background(), pool, uid, uid)
	if err != nil {
		t.Fatalf("first: %v", err)
	}

	second, err := RecordRequest(context.Background(), pool, uid, uid)
	if !errors.Is(err, ErrAlreadyRequested) {
		t.Fatalf("expected ErrAlreadyRequested, got %v", err)
	}
	if !second.Equal(first) {
		t.Errorf("second call should return original requested_at: %v vs %v", second, first)
	}
}

func TestAnonymiseAuthLocal_ScrubsUsersAndAuditLog(t *testing.T) {
	pool := startPG(t)
	ctx := context.Background()
	uid := seedUser(t, pool, "alice@x.dev")

	// Seed an audit row attributed to alice.
	if _, err := pool.Exec(ctx,
		`INSERT INTO audit_log (user_id, action) VALUES ($1::uuid, 'article.approved')`, uid,
	); err != nil {
		t.Fatalf("seed audit: %v", err)
	}

	if err := AnonymiseAuthLocal(ctx, pool, uid); err != nil {
		t.Fatalf("AnonymiseAuthLocal: %v", err)
	}

	// users row is scrubbed.
	var email, pw string
	var deletedAt *time.Time
	_ = pool.QueryRow(ctx,
		`SELECT email, password_hash, deleted_at FROM users WHERE id = $1::uuid`, uid,
	).Scan(&email, &pw, &deletedAt)
	if email == "alice@x.dev" {
		t.Errorf("email not scrubbed: %q", email)
	}
	if pw != "" {
		t.Errorf("password_hash not blanked: %q", pw)
	}
	if deletedAt == nil {
		t.Error("deleted_at not stamped")
	}

	// audit_log.user_id repointed at sentinel.
	var auditUID string
	_ = pool.QueryRow(ctx,
		`SELECT user_id::text FROM audit_log WHERE action = 'article.approved'`,
	).Scan(&auditUID)
	if auditUID != AnonymisedSentinelID {
		t.Errorf("audit_log.user_id = %q, want sentinel", auditUID)
	}
}

func TestAnonymiseAuthLocal_RefusesToScrubSentinelItself(t *testing.T) {
	pool := startPG(t)
	if err := AnonymiseAuthLocal(context.Background(), pool, AnonymisedSentinelID); err != nil {
		t.Fatalf("err: %v", err)
	}
	// Sentinel email must remain unchanged.
	var email string
	_ = pool.QueryRow(context.Background(),
		`SELECT email FROM users WHERE id = $1::uuid`, AnonymisedSentinelID,
	).Scan(&email)
	if email != "anonymised@deleted.local" {
		t.Errorf("sentinel email mutated: %q", email)
	}
}

func TestMarkServiceCompleted_PartialDoesNotSetCompletedAt(t *testing.T) {
	pool := startPG(t)
	uid := seedUser(t, pool, "alice@x.dev")
	if _, err := RecordRequest(context.Background(), pool, uid, uid); err != nil {
		t.Fatalf("record: %v", err)
	}

	expected := []string{"auth", "analytics"}
	if err := MarkServiceCompleted(context.Background(), pool, uid, "auth", expected); err != nil {
		t.Fatalf("mark: %v", err)
	}

	var completedAt *time.Time
	_ = pool.QueryRow(context.Background(),
		`SELECT completed_at FROM user_deletions WHERE user_id = $1::uuid`, uid,
	).Scan(&completedAt)
	if completedAt != nil {
		t.Error("completed_at must remain NULL when only 1 of 2 services has stamped")
	}
}

func TestMarkServiceCompleted_AllStampedSetsCompletedAt(t *testing.T) {
	pool := startPG(t)
	uid := seedUser(t, pool, "alice@x.dev")
	if _, err := RecordRequest(context.Background(), pool, uid, uid); err != nil {
		t.Fatalf("record: %v", err)
	}

	expected := []string{"auth", "analytics"}
	_ = MarkServiceCompleted(context.Background(), pool, uid, "auth", expected)
	_ = MarkServiceCompleted(context.Background(), pool, uid, "analytics", expected)

	var completedAt *time.Time
	_ = pool.QueryRow(context.Background(),
		`SELECT completed_at FROM user_deletions WHERE user_id = $1::uuid`, uid,
	).Scan(&completedAt)
	if completedAt == nil {
		t.Error("completed_at must be stamped once all services have stamped")
	}
}

func TestPendingOlderThan_ReturnsOnlyOldUnfinished(t *testing.T) {
	pool := startPG(t)
	ctx := context.Background()
	old1 := seedUser(t, pool, "old1@x.dev")
	old2 := seedUser(t, pool, "old2@x.dev")
	fresh := seedUser(t, pool, "fresh@x.dev")
	done := seedUser(t, pool, "done@x.dev")

	// Backdate two old + done; fresh stays now.
	for _, u := range []string{old1, old2, done} {
		_, _ = pool.Exec(ctx,
			`INSERT INTO user_deletions (user_id, requested_by, requested_at)
			 VALUES ($1::uuid, $1::uuid, now() - interval '40 days')`, u)
	}
	_, _ = pool.Exec(ctx,
		`INSERT INTO user_deletions (user_id, requested_by) VALUES ($1::uuid, $1::uuid)`, fresh)
	// Mark `done` complete.
	_, _ = pool.Exec(ctx,
		`UPDATE user_deletions SET completed_at = now() WHERE user_id = $1::uuid`, done)

	got, err := PendingOlderThan(ctx, pool, 25*24*time.Hour)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d ids, want 2: %v", len(got), got)
	}
	gotSet := map[string]bool{got[0]: true, got[1]: true}
	if !gotSet[old1] || !gotSet[old2] {
		t.Errorf("expected old1+old2, got %v", got)
	}
}
