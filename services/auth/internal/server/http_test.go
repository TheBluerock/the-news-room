package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	tcpg "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"

	casbinx "github.com/newsroom/auth/internal/casbin"
	jwtpkg "github.com/newsroom/auth/internal/jwt"
	"github.com/newsroom/auth/internal/store"
)

type fixture struct {
	mux *http.ServeMux
	jwt *jwtpkg.Manager
	db  *pgxpool.Pool
	rdb *redis.Client
}

func genPEM(t *testing.T) string {
	t.Helper()
	k, _ := rsa.GenerateKey(rand.Reader, 2048)
	der := x509.MarshalPKCS1PrivateKey(k)
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der}))
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()

	// Redis container
	rc, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		t.Skipf("redis testcontainer unavailable: %v", err)
	}
	t.Cleanup(func() { _ = rc.Terminate(context.Background()) })
	rhost, _ := rc.Host(ctx)
	rport, _ := rc.MappedPort(ctx, "6379/tcp")
	rdb, err := store.ConnectRedis(ctx, rhost+":"+rport.Port())
	if err != nil {
		t.Fatalf("redis: %v", err)
	}
	t.Cleanup(func() { _ = rdb.Close() })

	// Postgres container
	pc, err := tcpg.Run(ctx,
		"postgres:16-alpine",
		tcpg.WithDatabase("auth_test"),
		tcpg.WithUsername("auth"),
		tcpg.WithPassword("auth"),
		tcpg.BasicWaitStrategies(),
		tcpg.WithSQLDriver("pgx"),
	)
	if err != nil {
		t.Skipf("pg testcontainer unavailable: %v", err)
	}
	t.Cleanup(func() { _ = pc.Terminate(context.Background()) })
	dsn, _ := pc.ConnectionString(ctx, "sslmode=disable")
	db, err := store.ConnectPG(ctx, dsn)
	if err != nil {
		t.Fatalf("pg connect: %v", err)
	}
	t.Cleanup(db.Close)

	// Seed schema + users + casbin rules
	stmts := []string{
		`CREATE EXTENSION IF NOT EXISTS pgcrypto`,
		`CREATE TABLE users (
			id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			email         TEXT UNIQUE NOT NULL,
			market        TEXT,
			password_hash TEXT NOT NULL DEFAULT '',
			created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE casbin_rule (
			id    BIGSERIAL PRIMARY KEY,
			ptype TEXT NOT NULL,
			v0    TEXT, v1 TEXT, v2 TEXT,
			v3    TEXT, v4 TEXT, v5 TEXT
		)`,
		`CREATE TABLE audit_log (
			id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			timestamp   TIMESTAMPTZ NOT NULL DEFAULT now(),
			user_id     UUID NOT NULL,
			action      TEXT NOT NULL,
			resource_id UUID NOT NULL,
			market      TEXT,
			old_value   JSONB,
			new_value   JSONB
		)`,
		// password = "secret" bcrypt cost 4 — generated offline; cost 4 keeps tests fast.
		`INSERT INTO users (email, market, password_hash) VALUES
			('alice@x.dev', 'italy', crypt('secret', gen_salt('bf', 4))),
			('admin@x.dev', NULL,    crypt('admin-pw', gen_salt('bf', 4)))`,
		`INSERT INTO casbin_rule (ptype, v0, v1, v2) VALUES
			('p', 'admin',  '*',        '*'),
			('p', 'editor', 'articles', 'read')`,
		`INSERT INTO casbin_rule (ptype, v0, v1) VALUES
			('g', 'alice@x.dev', 'editor'),
			('g', 'admin@x.dev', 'admin')`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(ctx, s); err != nil {
			t.Fatalf("seed: %v\nSQL: %s", err, s)
		}
	}

	// JWT + Casbin
	jwtMgr, err := jwtpkg.NewManager(genPEM(t))
	if err != nil {
		t.Fatalf("jwt: %v", err)
	}
	rules, err := store.LoadCasbinRules(ctx, db)
	if err != nil {
		t.Fatalf("load rules: %v", err)
	}
	enf, err := casbinx.NewEnforcer(rules)
	if err != nil {
		t.Fatalf("enforcer: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	mux := NewHTTP(jwtMgr, db, rdb, enf, logger)
	return &fixture{mux: mux, jwt: jwtMgr, db: db, rdb: rdb}
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

func TestLogin_Success(t *testing.T) {
	fx := newFixture(t)
	rec := doJSON(t, fx.mux, "POST", "/api/auth/login",
		map[string]string{"email": "alice@x.dev", "password": "secret"}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var out map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&out)
	if out["access_token"] == "" || out["refresh_token"] == "" {
		t.Errorf("missing tokens: %v", out)
	}
	if out["token_type"] != "Bearer" {
		t.Errorf("token_type = %q", out["token_type"])
	}

	// access token must carry role=editor (from casbin g rule)
	claims, err := fx.jwt.Verify(out["access_token"])
	if err != nil {
		t.Fatalf("verify access: %v", err)
	}
	if claims.Role != "editor" {
		t.Errorf("role = %q", claims.Role)
	}
	if claims.Market != "italy" {
		t.Errorf("market = %q", claims.Market)
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	fx := newFixture(t)
	rec := doJSON(t, fx.mux, "POST", "/api/auth/login",
		map[string]string{"email": "alice@x.dev", "password": "WRONG"}, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestLogin_UnknownUser(t *testing.T) {
	fx := newFixture(t)
	rec := doJSON(t, fx.mux, "POST", "/api/auth/login",
		map[string]string{"email": "ghost@x.dev", "password": "secret"}, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestLogin_MalformedBody(t *testing.T) {
	fx := newFixture(t)
	req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewBufferString("{not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	fx.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestVerify_HappyPath(t *testing.T) {
	fx := newFixture(t)
	// Login → get access token
	loginRec := doJSON(t, fx.mux, "POST", "/api/auth/login",
		map[string]string{"email": "alice@x.dev", "password": "secret"}, nil)
	var out map[string]string
	_ = json.NewDecoder(loginRec.Body).Decode(&out)

	rec := doJSON(t, fx.mux, "GET", "/internal/verify", nil, map[string]string{
		"Authorization": "Bearer " + out["access_token"],
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Header().Get("X-User-Market") != "italy" {
		t.Errorf("X-User-Market = %q", rec.Header().Get("X-User-Market"))
	}
	if rec.Header().Get("X-User-Role") != "editor" {
		t.Errorf("X-User-Role = %q", rec.Header().Get("X-User-Role"))
	}
}

func TestVerify_MissingHeader(t *testing.T) {
	fx := newFixture(t)
	rec := doJSON(t, fx.mux, "GET", "/internal/verify", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestVerify_GarbageToken(t *testing.T) {
	fx := newFixture(t)
	rec := doJSON(t, fx.mux, "GET", "/internal/verify", nil, map[string]string{
		"Authorization": "Bearer not.a.jwt",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestVerify_BlockedJTI(t *testing.T) {
	fx := newFixture(t)
	loginRec := doJSON(t, fx.mux, "POST", "/api/auth/login",
		map[string]string{"email": "alice@x.dev", "password": "secret"}, nil)
	var out map[string]string
	_ = json.NewDecoder(loginRec.Body).Decode(&out)

	claims, _ := fx.jwt.Verify(out["access_token"])
	if err := store.Block(context.Background(), fx.rdb, claims.ID, 5*time.Minute); err != nil {
		t.Fatalf("Block: %v", err)
	}

	rec := doJSON(t, fx.mux, "GET", "/internal/verify", nil, map[string]string{
		"Authorization": "Bearer " + out["access_token"],
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("blocked token should yield 401, got %d", rec.Code)
	}
}

func TestRefresh_Success(t *testing.T) {
	fx := newFixture(t)
	loginRec := doJSON(t, fx.mux, "POST", "/api/auth/login",
		map[string]string{"email": "alice@x.dev", "password": "secret"}, nil)
	var login map[string]string
	_ = json.NewDecoder(loginRec.Body).Decode(&login)

	rec := doJSON(t, fx.mux, "POST", "/api/auth/refresh",
		map[string]string{"refresh_token": login["refresh_token"]}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var out map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&out)
	if out["access_token"] == "" {
		t.Error("expected new access_token")
	}
}

func TestRefresh_InvalidToken(t *testing.T) {
	fx := newFixture(t)
	rec := doJSON(t, fx.mux, "POST", "/api/auth/refresh",
		map[string]string{"refresh_token": "garbage"}, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestRefresh_RevokedJTI(t *testing.T) {
	fx := newFixture(t)
	loginRec := doJSON(t, fx.mux, "POST", "/api/auth/login",
		map[string]string{"email": "alice@x.dev", "password": "secret"}, nil)
	var login map[string]string
	_ = json.NewDecoder(loginRec.Body).Decode(&login)

	claims, _ := fx.jwt.Verify(login["refresh_token"])
	_ = store.Block(context.Background(), fx.rdb, claims.ID, 5*time.Minute)

	rec := doJSON(t, fx.mux, "POST", "/api/auth/refresh",
		map[string]string{"refresh_token": login["refresh_token"]}, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked refresh should yield 401, got %d", rec.Code)
	}
}

func TestRefresh_MalformedBody(t *testing.T) {
	fx := newFixture(t)
	req := httptest.NewRequest("POST", "/api/auth/refresh", bytes.NewBufferString("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	fx.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestAuditLog_Forbidden(t *testing.T) {
	fx := newFixture(t)
	rec := doJSON(t, fx.mux, "GET", "/api/admin/audit", nil, map[string]string{
		"X-User-Role": "editor",
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestAuditLog_AdminEmpty(t *testing.T) {
	fx := newFixture(t)
	rec := doJSON(t, fx.mux, "GET", "/api/admin/audit", nil, map[string]string{
		"X-User-Role": "admin",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&out)
	if entries, ok := out["entries"].([]any); !ok || len(entries) != 0 {
		t.Errorf("entries = %v", out["entries"])
	}
	if out["total"].(float64) != 0 {
		t.Errorf("total = %v", out["total"])
	}
}

func TestAuditLog_AdminWithRows(t *testing.T) {
	fx := newFixture(t)
	// Seed two audit rows
	_, err := fx.db.Exec(context.Background(),
		`INSERT INTO audit_log (user_id, action, resource_id, market, new_value)
		 VALUES (gen_random_uuid(), 'article.approved', gen_random_uuid(), 'italy', '{"k":"v"}'::jsonb),
		        (gen_random_uuid(), 'article.rejected', gen_random_uuid(), 'usa',   '{}'::jsonb)`)
	if err != nil {
		t.Fatalf("seed audit: %v", err)
	}

	rec := doJSON(t, fx.mux, "GET", "/api/admin/audit", nil, map[string]string{"X-User-Role": "admin"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var out map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&out)
	if out["total"].(float64) != 2 {
		t.Errorf("total = %v, want 2", out["total"])
	}

	// Filter by event_type
	rec = doJSON(t, fx.mux, "GET", "/api/admin/audit?event_type=article.approved", nil,
		map[string]string{"X-User-Role": "admin"})
	var filtered map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&filtered)
	if filtered["total"].(float64) != 1 {
		t.Errorf("filtered total = %v, want 1", filtered["total"])
	}
}

func TestAuditLog_PaginationLimitClamp(t *testing.T) {
	fx := newFixture(t)
	rec := doJSON(t, fx.mux, "GET", "/api/admin/audit?limit=9999&page=invalid", nil,
		map[string]string{"X-User-Role": "admin"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var out map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&out)
	if out["limit"].(float64) != 25 {
		t.Errorf("limit should clamp to default 25 (got %v)", out["limit"])
	}
	if out["page"].(float64) != 1 {
		t.Errorf("invalid page should default to 1 (got %v)", out["page"])
	}
}

func TestResolveRole_NoRolesDefaultsViewer(t *testing.T) {
	enf, _ := casbinx.NewEnforcer([][]string{
		{"p", "viewer", "articles", "read"},
	})
	if got := resolveRole(enf, "no-such-user@x.dev"); got != "viewer" {
		t.Errorf("got %q, want viewer", got)
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
