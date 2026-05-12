package adminhttp

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Server struct {
	db     *pgxpool.Pool
	logger *slog.Logger
}

func New(db *pgxpool.Pool, logger *slog.Logger) *http.Server {
	s := &Server{db: db, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/calendar/{market}", s.listCalendar)
	mux.HandleFunc("POST /api/calendar/{market}", s.createCalendar)
	mux.HandleFunc("DELETE /api/calendar/{market}/{id}", s.deleteCalendar)
	mux.HandleFunc("GET /api/analytics/market/{market}", s.getMarketAnalytics)
	return &http.Server{
		Addr:         ":8081",
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
	}
}

var validMarkets = map[string]bool{"italy": true, "usa": true, "china": true}

// calendarEntry mirrors the DB row shape returned to the admin frontend.
type calendarEntry struct {
	ID                   string  `json:"id"`
	Market               string  `json:"market"`
	TopicName            string  `json:"topic_name"`
	SourceURL            *string `json:"source_url"`
	Angle                *string `json:"angle"`
	JournalistProfileID  *string `json:"journalist_profile_id"`
	ScheduledAt          string  `json:"scheduled_at"`
	Dispatched           bool    `json:"dispatched"`
	CreatedAt            string  `json:"created_at"`
}

type marketAnalytics struct {
	Market               string         `json:"market"`
	ArticleCount30d      int            `json:"article_count_30d"`
	AvgQualityScore      float64        `json:"avg_quality_score"`
	PendingQueue         int            `json:"pending_queue"`
	TopRejectionReasons  []reasonCount  `json:"top_rejection_reasons"`
}

type reasonCount struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

func (s *Server) listCalendar(w http.ResponseWriter, r *http.Request) {
	market := r.PathValue("market")
	if !validMarkets[market] {
		writeErr(w, http.StatusBadRequest, "invalid market")
		return
	}

	rows, err := s.db.Query(r.Context(), `
		SELECT id::text, market, topic_name,
		       source_url, angle, journalist_profile_id::text,
		       scheduled_at, dispatched, created_at
		FROM editorial_calendar
		WHERE market = $1
		ORDER BY scheduled_at DESC
		LIMIT 200
	`, market)
	if err != nil {
		s.logger.Error("list calendar", "err", err)
		writeErr(w, http.StatusInternalServerError, "db error")
		return
	}
	defer rows.Close()

	var entries []calendarEntry
	for rows.Next() {
		var e calendarEntry
		var scheduledAt, createdAt time.Time
		var jpID *string
		if err := rows.Scan(
			&e.ID, &e.Market, &e.TopicName,
			&e.SourceURL, &e.Angle, &jpID,
			&scheduledAt, &e.Dispatched, &createdAt,
		); err != nil {
			continue
		}
		e.ScheduledAt = scheduledAt.UTC().Format(time.RFC3339)
		e.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		e.JournalistProfileID = jpID
		entries = append(entries, e)
	}
	if entries == nil {
		entries = []calendarEntry{}
	}
	writeJSON(w, http.StatusOK, entries)
}

type createCalendarReq struct {
	TopicName           string `json:"topic_name"`
	ScheduledAt         string `json:"scheduled_at"`
	Angle               string `json:"angle"`
	SourceURL           string `json:"source_url"`
	JournalistProfileID string `json:"journalist_profile_id"`
}

func (s *Server) createCalendar(w http.ResponseWriter, r *http.Request) {
	market := r.PathValue("market")
	if !validMarkets[market] {
		writeErr(w, http.StatusBadRequest, "invalid market")
		return
	}
	if !isAdmin(r) {
		writeErr(w, http.StatusForbidden, "admin role required")
		return
	}

	var req createCalendarReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.TopicName == "" || req.ScheduledAt == "" {
		writeErr(w, http.StatusBadRequest, "topic_name and scheduled_at are required")
		return
	}

	scheduledAt, err := time.Parse(time.RFC3339, req.ScheduledAt)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "scheduled_at must be RFC3339")
		return
	}

	topicID := uuid.NewString()
	var angle, sourceURL, jpID *string
	if req.Angle != "" {
		angle = &req.Angle
	}
	if req.SourceURL != "" {
		sourceURL = &req.SourceURL
	}
	if req.JournalistProfileID != "" {
		jpID = &req.JournalistProfileID
	}

	var id string
	err = s.db.QueryRow(r.Context(), `
		INSERT INTO editorial_calendar
		    (market, topic_id, topic_name, scheduled_at, angle, source_url, journalist_profile_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7::uuid)
		RETURNING id::text
	`, market, topicID, req.TopicName, scheduledAt, angle, sourceURL, jpID).Scan(&id)
	if err != nil {
		s.logger.Error("create calendar entry", "err", err)
		writeErr(w, http.StatusInternalServerError, "db error")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (s *Server) deleteCalendar(w http.ResponseWriter, r *http.Request) {
	market := r.PathValue("market")
	id := r.PathValue("id")
	if !validMarkets[market] {
		writeErr(w, http.StatusBadRequest, "invalid market")
		return
	}
	if !isAdmin(r) {
		writeErr(w, http.StatusForbidden, "admin role required")
		return
	}

	tag, err := s.db.Exec(r.Context(), `
		DELETE FROM editorial_calendar
		WHERE id::text = $1 AND market = $2 AND dispatched = false
	`, id, market)
	if err != nil {
		s.logger.Error("delete calendar entry", "err", err)
		writeErr(w, http.StatusInternalServerError, "db error")
		return
	}
	if tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "entry not found or already dispatched")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getMarketAnalytics(w http.ResponseWriter, r *http.Request) {
	market := r.PathValue("market")
	if !validMarkets[market] {
		writeErr(w, http.StatusBadRequest, "invalid market")
		return
	}

	result := marketAnalytics{
		Market:              market,
		TopRejectionReasons: []reasonCount{},
	}

	// Article count + avg quality from analytics_svc.article_performance
	err := s.db.QueryRow(r.Context(), `
		SELECT
		    COUNT(*) FILTER (WHERE created_at > now() - INTERVAL '30 days'),
		    COALESCE(AVG(quality_score) FILTER (WHERE created_at > now() - INTERVAL '30 days'), 0)
		FROM article_performance
		WHERE market = $1
	`, market).Scan(&result.ArticleCount30d, &result.AvgQualityScore)
	if err != nil {
		s.logger.Error("market analytics query", "err", err)
		writeErr(w, http.StatusInternalServerError, "db error")
		return
	}

	// Pending queue count from moderation_svc.review_queue
	err = s.db.QueryRow(r.Context(), `
		SELECT COUNT(*) FROM moderation_svc.review_queue
		WHERE market = $1 AND status IN ('auto_rejected', 'human_rejected')
		  AND created_at > now() - INTERVAL '7 days'
	`, market).Scan(&result.PendingQueue)
	if err != nil {
		// Non-fatal — moderation_svc may not exist yet if migration 007 not run
		s.logger.Warn("pending queue query failed — migration 007 pending?", "err", err)
	}

	// Top rejection reasons from moderation_svc.review_queue
	rows, err := s.db.Query(r.Context(), `
		SELECT reason, COUNT(*) as cnt
		FROM (
		    SELECT unnest(rejection_reasons) as reason
		    FROM moderation_svc.review_queue
		    WHERE market = $1
		      AND status IN ('auto_rejected', 'human_rejected')
		      AND created_at > now() - INTERVAL '30 days'
		) sub
		GROUP BY reason
		ORDER BY cnt DESC
		LIMIT 5
	`, market)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var rc reasonCount
			if err := rows.Scan(&rc.Reason, &rc.Count); err == nil {
				result.TopRejectionReasons = append(result.TopRejectionReasons, rc)
			}
		}
	}

	writeJSON(w, http.StatusOK, result)
}

func isAdmin(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("X-User-Role"), "admin")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// Start starts the HTTP server in a goroutine and returns the server for graceful shutdown.
func Start(srv *http.Server, logger *slog.Logger) {
	go func() {
		logger.Info("admin HTTP server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("admin HTTP server error", "err", err)
		}
	}()
}
