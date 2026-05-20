package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/casbin/casbin/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"

	jwtpkg "github.com/newsroom/auth/internal/jwt"
	"github.com/newsroom/auth/internal/store"
)

type HTTPServer struct {
	jwt      *jwtpkg.Manager
	db       *pgxpool.Pool
	rdb      *redis.Client
	enforcer *casbin.Enforcer
	logger   *slog.Logger
}

func NewHTTP(
	jwt *jwtpkg.Manager,
	db *pgxpool.Pool,
	rdb *redis.Client,
	enforcer *casbin.Enforcer,
	logger *slog.Logger,
) *http.ServeMux {
	s := &HTTPServer{jwt: jwt, db: db, rdb: rdb, enforcer: enforcer, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/refresh", s.refresh)
	mux.HandleFunc("GET /internal/verify", s.verify)
	mux.HandleFunc("GET /api/admin/audit", s.auditLog)
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
