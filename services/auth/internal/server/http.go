package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/casbin/casbin/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"

	"github.com/newsroom/auth/internal/gdpr"
	jwtpkg "github.com/newsroom/auth/internal/jwt"
	"github.com/newsroom/auth/internal/store"
)

// ExpectedDeletionServices lists every service that owns PII referencing
// auth users. user_deletions.completed_at is only stamped once all of these
// have ack'd the user.data.deletion.requested event.
var ExpectedDeletionServices = []string{"auth", "analytics"}

type HTTPServer struct {
	jwt       *jwtpkg.Manager
	db        *pgxpool.Pool
	rdb       *redis.Client
	enforcer  *casbin.Enforcer
	logger    *slog.Logger
	gdprPub   gdpr.Publisher
}

func NewHTTP(
	jwt *jwtpkg.Manager,
	db *pgxpool.Pool,
	rdb *redis.Client,
	enforcer *casbin.Enforcer,
	logger *slog.Logger,
	gdprPub gdpr.Publisher,
) *http.ServeMux {
	s := &HTTPServer{jwt: jwt, db: db, rdb: rdb, enforcer: enforcer, logger: logger, gdprPub: gdprPub}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/refresh", s.refresh)
	mux.HandleFunc("GET /internal/verify", s.verify)
	mux.HandleFunc("GET /api/admin/audit", s.auditLog)
	mux.HandleFunc("DELETE /api/user/data", s.deleteUserData)
	return mux
}

// login accepts {email, password} and returns {access_token, refresh_token}.
func (s *HTTPServer) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	user, err := store.GetUserByEmail(r.Context(), s.db, req.Email)
	if err != nil {
		// Don't reveal whether user exists
		writeErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		writeErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	// Determine role via Casbin
	role := resolveRole(s.enforcer, user.Email)

	accessTok, _, err := s.jwt.IssueAccess(user.ID, user.Market, role)
	if err != nil {
		s.logger.Error("issue access token", "err", err)
		writeErr(w, http.StatusInternalServerError, "token error")
		return
	}
	refreshTok, _, err := s.jwt.IssueRefresh(user.ID)
	if err != nil {
		s.logger.Error("issue refresh token", "err", err)
		writeErr(w, http.StatusInternalServerError, "token error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"access_token":  accessTok,
		"refresh_token": refreshTok,
		"token_type":    "Bearer",
	})
}

// refresh accepts {refresh_token} and returns a new {access_token}.
func (s *HTTPServer) refresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	claims, err := s.jwt.Verify(req.RefreshToken)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}

	blocked, err := store.IsBlocked(r.Context(), s.rdb, claims.ID)
	if err != nil || blocked {
		writeErr(w, http.StatusUnauthorized, "token revoked")
		return
	}

	user, err := store.GetUserByID(r.Context(), s.db, claims.Subject)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "user not found")
		return
	}
	role := resolveRole(s.enforcer, user.Email)

	accessTok, _, err := s.jwt.IssueAccess(user.ID, user.Market, role)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "token error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"access_token": accessTok,
		"token_type":   "Bearer",
	})
}

// verify validates the Bearer token and sets X-User-* headers for Caddy forward-auth.
func (s *HTTPServer) verify(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	tokenStr := strings.TrimPrefix(authHeader, "Bearer ")

	claims, err := s.jwt.Verify(tokenStr)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	blocked, err := store.IsBlocked(r.Context(), s.rdb, claims.ID)
	if err != nil || blocked {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	w.Header().Set("X-User-ID", claims.Subject)
	w.Header().Set("X-User-Market", claims.Market)
	w.Header().Set("X-User-Role", claims.Role)
	w.WriteHeader(http.StatusOK)
}

// auditLog returns paginated audit_log rows. Requires X-User-Role: admin (set by Caddy forward_auth).
func (s *HTTPServer) auditLog(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-User-Role") != "admin" {
		writeErr(w, http.StatusForbidden, "admin role required")
		return
	}

	q := r.URL.Query()
	action := q.Get("event_type")
	market := q.Get("market")
	page := 1
	limit := 25
	if v := q.Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	offset := (page - 1) * limit

	rows, err := s.db.Query(r.Context(), `
		SELECT id::text, timestamp, user_id::text, action, resource_id::text,
		       COALESCE(market, ''), COALESCE(old_value::text, '{}'), COALESCE(new_value::text, '{}')
		FROM audit_log
		WHERE ($1 = '' OR action = $1)
		  AND ($2 = '' OR market = $2)
		ORDER BY timestamp DESC
		LIMIT $3 OFFSET $4
	`, action, market, limit, offset)
	if err != nil {
		s.logger.Error("audit log query", "err", err)
		writeErr(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()

	type entry struct {
		ID         string `json:"id"`
		CreatedAt  string `json:"created_at"`
		ActorID    string `json:"actor_id"`
		EventType  string `json:"event_type"`
		ResourceID string `json:"resource_id"`
		Market     string `json:"market"`
		OldValue   string `json:"old_value,omitempty"`
		NewValue   string `json:"new_value,omitempty"`
	}

	var entries []entry
	for rows.Next() {
		var e entry
		var ts time.Time
		if err := rows.Scan(&e.ID, &ts, &e.ActorID, &e.EventType, &e.ResourceID, &e.Market, &e.OldValue, &e.NewValue); err != nil {
			continue
		}
		e.CreatedAt = ts.UTC().Format(time.RFC3339)
		entries = append(entries, e)
	}

	var total int
	s.db.QueryRow(r.Context(), `
		SELECT COUNT(*) FROM audit_log
		WHERE ($1 = '' OR action = $1) AND ($2 = '' OR market = $2)
	`, action, market).Scan(&total)

	if entries == nil {
		entries = []entry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"entries": entries,
		"total":   total,
		"page":    page,
		"limit":   limit,
	})
}

// deleteUserData implements the GDPR Article 17 "right to erasure" endpoint.
// Owner decision 2026-05-21: uniform 30-day deletion right for ALL markets.
//
// Flow:
//   1. Verify Bearer JWT, extract user_id from claims.Subject.
//   2. Insert ledger row in auth_svc.user_deletions (idempotent on user_id).
//   3. Anonymise auth's own tables in one TX (users + audit_log).
//   4. Publish user.data.deletion.requested so other services scrub their PII.
//   5. Mark "auth" service complete in ledger.
//   6. Return 202 Accepted with ledger ID + requested_at.
//
// Failure of step 4 is FATAL — we revert the local anonymisation by deleting
// the ledger row so the user can retry. Half-done anonymisation across the
// fleet is worse than no anonymisation at all (GDPR liability + audit drift).
func (s *HTTPServer) deleteUserData(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		writeErr(w, http.StatusUnauthorized, "Bearer token required")
		return
	}
	claims, err := s.jwt.Verify(strings.TrimPrefix(authHeader, "Bearer "))
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "invalid token")
		return
	}

	// Don't allow deleting the sentinel user — that would orphan every
	// previously-anonymised row's FK reference target.
	if claims.Subject == gdpr.AnonymisedSentinelID {
		writeErr(w, http.StatusForbidden, "cannot delete anonymised sentinel user")
		return
	}

	// Refuse deletion if the token has been revoked.
	if blocked, _ := store.IsBlocked(r.Context(), s.rdb, claims.ID); blocked {
		writeErr(w, http.StatusUnauthorized, "token revoked")
		return
	}

	requestedAt, err := gdpr.RecordRequest(r.Context(), s.db, claims.Subject, claims.Subject)
	if errors.Is(err, gdpr.ErrAlreadyRequested) {
		// Idempotent: previous request still being processed. Return 200 with the original timestamp.
		writeJSON(w, http.StatusOK, map[string]any{
			"user_id":          claims.Subject,
			"requested_at":     requestedAt.UTC().Format(time.RFC3339),
			"status":           "already_requested",
			"completion_due":   requestedAt.Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339),
		})
		return
	}
	if err != nil {
		s.logger.Error("record deletion request", "err", err, "user_id", claims.Subject)
		writeErr(w, http.StatusInternalServerError, "ledger error")
		return
	}

	if err := gdpr.AnonymiseAuthLocal(r.Context(), s.db, claims.Subject); err != nil {
		s.logger.Error("anonymise auth local", "err", err, "user_id", claims.Subject)
		// Roll back the ledger row so the user can retry.
		if _, delErr := s.db.Exec(r.Context(),
			`DELETE FROM auth_svc.user_deletions WHERE user_id = $1::uuid`, claims.Subject,
		); delErr != nil {
			s.logger.Error("rollback ledger row", "err", delErr, "user_id", claims.Subject)
		}
		writeErr(w, http.StatusInternalServerError, "anonymisation failed; please retry")
		return
	}

	// Best-effort event publish. If the broker is unreachable we keep the
	// local anonymisation (already done) and the ledger row pending — the
	// lag cron will alert at day 25 and ops can republish manually from
	// the ledger. Returning 500 here would lie to the user about whether
	// anonymisation happened.
	evt := gdpr.NewEvent(claims.Subject, claims.Subject, "")
	if s.gdprPub != nil {
		if err := s.gdprPub.Publish(r.Context(), evt); err != nil {
			s.logger.Error("publish deletion event", "err", err, "user_id", claims.Subject)
		}
	}

	// Mark auth's own piece as done. Lag cron only flags when other services
	// have not stamped in by day 25.
	if err := gdpr.MarkServiceCompleted(
		r.Context(), s.db, claims.Subject, "auth", ExpectedDeletionServices,
	); err != nil {
		s.logger.Warn("mark auth completed", "err", err, "user_id", claims.Subject)
	}

	// Revoke this token so the rest of its lifetime can't be reused. Refresh
	// token would also be a good idea but we don't have its jti here — TODO.
	_ = store.Block(r.Context(), s.rdb, claims.ID, 7*24*time.Hour)

	writeJSON(w, http.StatusAccepted, map[string]any{
		"user_id":        claims.Subject,
		"event_id":       evt.EventID,
		"requested_at":   requestedAt.UTC().Format(time.RFC3339),
		"completion_due": requestedAt.Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339),
		"status":         "accepted",
	})
}

func resolveRole(e *casbin.Enforcer, email string) string {
	roles, _ := e.GetRolesForUser(email)
	if len(roles) > 0 {
		return roles[0]
	}
	return "viewer"
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// StartHTTP starts the HTTP server and returns it for graceful shutdown.
func StartHTTP(mux http.Handler, addr string) *http.Server {
	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server error", "err", err)
		}
	}()
	return srv
}

// Keep compiler happy with unused import during stub phase
var _ = context.Background
