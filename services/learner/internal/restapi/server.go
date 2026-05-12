package restapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/newsroom/learner/internal/db"
	"github.com/newsroom/learner/internal/fastpath"
)

type Server struct {
	pool   *pgxpool.Pool
	rdb    *redis.Client
	logger *slog.Logger
}

func New(pool *pgxpool.Pool, rdb *redis.Client, logger *slog.Logger) *http.Server {
	s := &Server{pool: pool, rdb: rdb, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /suggestions", s.suggestions)
	mux.HandleFunc("GET /api/quality-summary", s.qualitySummary)
	mux.HandleFunc("POST /api/corrections", s.corrections)
	srv := &http.Server{
		Addr:         ":8088",
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	return srv
}

func Start(srv *http.Server, logger *slog.Logger) {
	go func() {
		logger.Info("learner REST server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("learner REST server error", "err", err)
		}
	}()
}

var validMarkets = map[string]bool{"italy": true, "usa": true, "china": true}

func (s *Server) suggestions(w http.ResponseWriter, r *http.Request) {
	market := r.URL.Query().Get("market")
	if market == "" {
		writeErr(w, http.StatusBadRequest, "market required")
		return
	}
	limit := 10
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 50 {
			limit = n
		}
	}
	rows, err := db.GetTopicSuggestions(r.Context(), s.pool, market, limit)
	if err != nil {
		s.logger.Error("get topic suggestions", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) qualitySummary(w http.ResponseWriter, r *http.Request) {
	market := r.URL.Query().Get("market")
	if !validMarkets[market] {
		writeErr(w, http.StatusBadRequest, "valid market required")
		return
	}
	summary, err := db.GetMarketQualitySummary(r.Context(), s.pool, market)
	if err != nil {
		s.logger.Error("quality summary query", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

type correctionReq struct {
	ArticleID  string `json:"article_id"`
	Market     string `json:"market"`
	Correction string `json:"correction"`
}

func (s *Server) corrections(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-User-Role") != "admin" {
		writeErr(w, http.StatusForbidden, "admin role required")
		return
	}

	var req correctionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	req.ArticleID = strings.TrimSpace(req.ArticleID)
	req.Market = strings.TrimSpace(req.Market)
	req.Correction = strings.TrimSpace(req.Correction)

	if req.ArticleID == "" || req.Market == "" || req.Correction == "" {
		writeErr(w, http.StatusBadRequest, "article_id, market, and correction are required")
		return
	}
	if !validMarkets[req.Market] {
		writeErr(w, http.StatusBadRequest, "invalid market")
		return
	}

	correctionID := uuid.NewString()
	payload := map[string]interface{}{
		"correction_id": correctionID,
		"article_id":    req.ArticleID,
		"correction":    req.Correction,
		"updated_at":    time.Now().UTC().Format(time.RFC3339),
	}

	// Fast path: Redis (agent reads on next run)
	if err := fastpath.WriteCorrection(r.Context(), s.rdb, req.Market, correctionID, payload, s.logger); err != nil {
		s.logger.Error("fastpath write correction", "err", err)
		writeErr(w, http.StatusInternalServerError, "redis error")
		return
	}

	// Slow path: PostgreSQL log (non-fatal if it fails)
	if err := db.LogCorrection(
		r.Context(), s.pool,
		correctionID, req.Market, "editorial", req.Correction, "", req.Correction,
	); err != nil {
		s.logger.Warn("slow-path correction log failed", "err", err)
	}

	s.logger.Info("correction submitted", "article_id", req.ArticleID, "market", req.Market, "correction_id", correctionID)
	writeJSON(w, http.StatusOK, map[string]string{"correction_id": correctionID})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
