package store

import (
	"context"
	"testing"
	"time"

	tcpg "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcwait "github.com/testcontainers/testcontainers-go/wait"
)

// startPG launches a fresh Postgres 16 container, applies the minimum schema
// the auth service queries against, and seeds a known user + Casbin rules.
func startPG(t *testing.T) string {
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
		t.Skipf("testcontainers/postgres unavailable: %v", err)
	}
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })

	// Wait for Postgres to accept connections (BasicWaitStrategies covers the rest).
	if err := tcwait.ForLog("database system is ready to accept connections").
		WithOccurrence(2).
		WithStartupTimeout(60 * time.Second).
		WaitUntilReady(ctx, c); err != nil {
		t.Fatalf("pg wait: %v", err)
	}

	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("dsn: %v", err)
	}
	return dsn
}

// applySchema applies the bits of migrations 001 + 002 the auth service touches.
// We intentionally do not pull in the full migration runner — keeps tests fast
// and removes a moving dependency on tooling install path.
func applySchema(t *testing.T, dsn string) {
	t.Helper()
	pool, err := ConnectPG(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	stmts := []string{
		`CREATE EXTENSION IF NOT EXISTS pgcrypto`,
		`CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`,
		`CREATE TABLE users (
			id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			email         TEXT UNIQUE NOT NULL,
			market        TEXT CHECK (market IN ('italy', 'usa', 'china')),
			password_hash TEXT NOT NULL DEFAULT '',
			created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE casbin_rule (
			id    BIGSERIAL PRIMARY KEY,
			ptype TEXT NOT NULL,
			v0    TEXT, v1 TEXT, v2 TEXT,
			v3    TEXT, v4 TEXT, v5 TEXT
		)`,
		`INSERT INTO users (email, market, password_hash) VALUES
			('admin@newsroom.dev',        NULL,    crypt('dev-admin-123',  gen_salt('bf', 4))),
			('editor.italy@newsroom.dev', 'italy', crypt('dev-editor-123', gen_salt('bf', 4)))`,
		`INSERT INTO casbin_rule (ptype, v0, v1, v2) VALUES
			('p', 'admin',  '*',        '*'),
			('p', 'editor', 'articles', 'read'),
			('p', 'editor', 'articles', 'approve')`,
		`INSERT INTO casbin_rule (ptype, v0, v1) VALUES
			('g', 'admin@newsroom.dev',        'admin'),
			('g', 'editor.italy@newsroom.dev', 'editor')`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(context.Background(), s); err != nil {
			t.Fatalf("apply schema: %v\nSQL: %s", err, s)
		}
	}
}

func TestConnectPG_OK(t *testing.T) {
	dsn := startPG(t)
	pool, err := ConnectPG(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
}

func TestConnectPG_BadDSN(t *testing.T) {
	_, err := ConnectPG(context.Background(), "postgres://nobody:nobody@127.0.0.1:1/none?sslmode=disable&connect_timeout=1")
	if err == nil {
		t.Fatal("expected error on dead DSN")
	}
}

func TestGetUserByEmail_Found(t *testing.T) {
	dsn := startPG(t)
	applySchema(t, dsn)
	pool, _ := ConnectPG(context.Background(), dsn)
	defer pool.Close()

	u, err := GetUserByEmail(context.Background(), pool, "editor.italy@newsroom.dev")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if u.Email != "editor.italy@newsroom.dev" {
		t.Errorf("email = %q", u.Email)
	}
	if u.Market != "italy" {
		t.Errorf("market = %q", u.Market)
	}
	if u.ID == "" {
		t.Error("id must be populated")
	}
	if u.PasswordHash == "" {
		t.Error("password_hash must be populated")
	}
}

func TestGetUserByEmail_NullMarketCoalesced(t *testing.T) {
	dsn := startPG(t)
	applySchema(t, dsn)
	pool, _ := ConnectPG(context.Background(), dsn)
	defer pool.Close()

	u, err := GetUserByEmail(context.Background(), pool, "admin@newsroom.dev")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if u.Market != "" {
		t.Errorf("admin market should COALESCE to empty, got %q", u.Market)
	}
}

func TestGetUserByEmail_NotFound(t *testing.T) {
	dsn := startPG(t)
	applySchema(t, dsn)
	pool, _ := ConnectPG(context.Background(), dsn)
	defer pool.Close()

	if _, err := GetUserByEmail(context.Background(), pool, "ghost@nowhere.dev"); err == nil {
		t.Fatal("expected error for missing user")
	}
}

func TestLoadCasbinRules_Seeded(t *testing.T) {
	dsn := startPG(t)
	applySchema(t, dsn)
	pool, _ := ConnectPG(context.Background(), dsn)
	defer pool.Close()

	rules, err := LoadCasbinRules(context.Background(), pool)
	if err != nil {
		t.Fatalf("LoadCasbinRules: %v", err)
	}
	if len(rules) != 5 {
		t.Fatalf("got %d rules, want 5", len(rules))
	}
	// Verify trailing empty cols stripped.
	for _, r := range rules {
		if r[len(r)-1] == "" {
			t.Errorf("rule has trailing empty: %v", r)
		}
	}
	// Verify admin grant.
	var found bool
	for _, r := range rules {
		if r[0] == "g" && r[1] == "admin@newsroom.dev" && r[2] == "admin" {
			found = true
		}
	}
	if !found {
		t.Errorf("admin grant not found in %v", rules)
	}
}

func TestLoadCasbinRules_Empty(t *testing.T) {
	dsn := startPG(t)
	pool, _ := ConnectPG(context.Background(), dsn)
	defer pool.Close()
	// Create empty table only.
	_, _ = pool.Exec(context.Background(), `CREATE TABLE casbin_rule (
		id    BIGSERIAL PRIMARY KEY,
		ptype TEXT NOT NULL,
		v0    TEXT, v1 TEXT, v2 TEXT,
		v3    TEXT, v4 TEXT, v5 TEXT
	)`)
	rules, err := LoadCasbinRules(context.Background(), pool)
	if err != nil {
		t.Fatalf("LoadCasbinRules: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("expected empty, got %v", rules)
	}
}
