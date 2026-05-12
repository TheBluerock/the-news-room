package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	pgvector "github.com/pgvector/pgvector-go"
	pgxvector "github.com/pgvector/pgvector-go/pgx"
)

// KnowledgeNode is a result from a knowledge graph query.
type KnowledgeNode struct {
	ID      string
	Type    string  // "article" | "source" | "journalist"
	Content string
	Weight  float32
}

// ProfileRow is a journalist profile record.
type ProfileRow struct {
	JournalistID   string
	Market         string
	Specialization string
	StyleProfile   []byte // JSON
}

// RegisterTypes must be called once per pgxpool to enable pgvector support.
func RegisterTypes(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	return pgxvector.RegisterTypes(ctx, conn.Conn())
}

// UpsertSource stores a scraped source with its embedding.
// Deduplicates by url_hash; updates content and embedding if URL already exists.
func UpsertSource(ctx context.Context, pool *pgxpool.Pool, market, url, title, content string, embedding []float32) error {
	vec := pgvector.NewVector(embedding)
	_, err := pool.Exec(ctx, `
		INSERT INTO learner_svc.sources (market, url, title, content, embedding, fetched_at)
		VALUES ($1, $2, $3, $4, $5, now())
		ON CONFLICT (url) DO UPDATE
		  SET content   = EXCLUDED.content,
		      embedding = EXCLUDED.embedding,
		      fetched_at = now()
	`, market, url, title, content, vec)
	return err
}

// UpsertEmbedding stores an article embedding and updates the Redis HNSW stale marker.
func UpsertEmbedding(ctx context.Context, pool *pgxpool.Pool, articleID, market string, embedding []float32) error {
	vec := pgvector.NewVector(embedding)
	_, err := pool.Exec(ctx, `
		INSERT INTO learner_svc.article_embeddings (article_id, market, embedding, stale, updated_at)
		VALUES ($1, $2, $3, false, now())
		ON CONFLICT (article_id) DO UPDATE
		  SET embedding  = EXCLUDED.embedding,
		      stale      = false,
		      updated_at = now()
	`, articleID, market, vec)
	return err
}

// GetJournalistProfile fetches a journalist profile by ID.
func GetJournalistProfile(ctx context.Context, pool *pgxpool.Pool, id string) (*ProfileRow, error) {
	row := pool.QueryRow(ctx, `
		SELECT journalist_id, market, specialization, style_profile
		FROM learner_svc.journalist_profiles
		WHERE journalist_id = $1
	`, id)

	var p ProfileRow
	if err := row.Scan(&p.JournalistID, &p.Market, &p.Specialization, &p.StyleProfile); err != nil {
		return nil, fmt.Errorf("journalist profile %s: %w", id, err)
	}
	return &p, nil
}

// SearchSimilar runs pgvector cosine similarity search on article embeddings and sources.
func SearchSimilar(ctx context.Context, pool *pgxpool.Pool, market string, queryVec []float32, limit int) ([]KnowledgeNode, error) {
	vec := pgvector.NewVector(queryVec)

	// JOIN article_performance to weight results by editorial quality score.
	// Articles with no recorded quality default to 0.5 (neutral).
	rows, err := pool.Query(ctx, `
		SELECT ae.article_id::text, 'article' AS type,
		       '' AS content,
		       ((1 - (ae.embedding <=> $1)) * COALESCE(ap.quality_score, 0.5))::float4 AS weight
		FROM learner_svc.article_embeddings ae
		LEFT JOIN analytics_svc.article_performance ap
		       ON ap.article_id = ae.article_id
		WHERE ae.market = $2
		  AND ae.stale = false
		  AND ae.embedding IS NOT NULL
		ORDER BY ae.embedding <=> $1
		LIMIT $3
	`, vec, market, limit)
	if err != nil {
		return nil, fmt.Errorf("vector search articles: %w", err)
	}
	defer rows.Close()

	var results []KnowledgeNode
	for rows.Next() {
		var n KnowledgeNode
		if err := rows.Scan(&n.ID, &n.Type, &n.Content, &n.Weight); err != nil {
			continue
		}
		results = append(results, n)
	}

	// Also search sources
	srcRows, err := pool.Query(ctx, `
		SELECT id::text, 'source' AS type,
		       title || '. ' || left(content, 500) AS content,
		       (1 - (embedding <=> $1))::float4 AS weight
		FROM learner_svc.sources
		WHERE market = $2
		  AND embedding IS NOT NULL
		ORDER BY embedding <=> $1
		LIMIT $3
	`, vec, market, limit)
	if err != nil {
		return nil, fmt.Errorf("vector search sources: %w", err)
	}
	defer srcRows.Close()

	for srcRows.Next() {
		var n KnowledgeNode
		if err := srcRows.Scan(&n.ID, &n.Type, &n.Content, &n.Weight); err != nil {
			continue
		}
		results = append(results, n)
	}
	return results, nil
}

// SearchFullText runs a PostgreSQL full-text search on sources.
func SearchFullText(ctx context.Context, pool *pgxpool.Pool, market, query string, limit int) ([]KnowledgeNode, error) {
	rows, err := pool.Query(ctx, `
		SELECT id::text, 'source' AS type,
		       title || '. ' || left(content, 500) AS content,
		       ts_rank(to_tsvector('english', coalesce(title,'') || ' ' || coalesce(content,'')),
		               plainto_tsquery('english', $1))::float4 AS weight
		FROM learner_svc.sources
		WHERE market = $2
		  AND to_tsvector('english', coalesce(title,'') || ' ' || coalesce(content,''))
		      @@ plainto_tsquery('english', $1)
		ORDER BY weight DESC
		LIMIT $3
	`, query, market, limit)
	if err != nil {
		return nil, fmt.Errorf("full-text search: %w", err)
	}
	defer rows.Close()

	var results []KnowledgeNode
	for rows.Next() {
		var n KnowledgeNode
		if err := rows.Scan(&n.ID, &n.Type, &n.Content, &n.Weight); err != nil {
			continue
		}
		results = append(results, n)
	}
	return results, nil
}

// QualitySummary holds market-level quality signal for the agent prompt.
type QualitySummary struct {
	Market            string
	AvgQualityScore   float64
	ArticleCount30d   int
	TopRejections     []string
}

// GetMarketQualitySummary queries article_performance and learner rejections to
// produce a quality signal for a given market, used in agent prompt construction.
func GetMarketQualitySummary(ctx context.Context, pool *pgxpool.Pool, market string) (*QualitySummary, error) {
	s := &QualitySummary{Market: market}

	err := pool.QueryRow(ctx, `
		SELECT COALESCE(AVG(quality_score), 0.5), COUNT(*)
		FROM analytics_svc.article_performance
		WHERE market = $1
		  AND created_at >= now() - INTERVAL '30 days'
	`, market).Scan(&s.AvgQualityScore, &s.ArticleCount30d)
	if err != nil {
		return nil, fmt.Errorf("quality summary query: %w", err)
	}

	rows, err := pool.Query(ctx, `
		SELECT reason
		FROM learner_svc.rejections
		WHERE market = $1
		  AND logged_at >= now() - INTERVAL '30 days'
		GROUP BY reason
		ORDER BY COUNT(*) DESC
		LIMIT 5
	`, market)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var reason string
			if rows.Scan(&reason) == nil {
				s.TopRejections = append(s.TopRejections, reason)
			}
		}
	}

	return s, nil
}

// LogCorrection writes a correction to the slow-path PostgreSQL log.
func LogCorrection(ctx context.Context, pool *pgxpool.Pool, correctionID, market, correctionType, reason, oldValue, newValue string) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO learner_svc.corrections
		  (correction_id, market, correction_type, reason, old_value, new_value, applied_at)
		VALUES ($1, $2, $3, $4, $5, $6, now())
		ON CONFLICT (correction_id) DO NOTHING
	`, correctionID, market, correctionType, reason, oldValue, newValue)
	return err
}

// LogRejection writes a moderation rejection to the slow-path log.
func LogRejection(ctx context.Context, pool *pgxpool.Pool, articleID, market, reason string) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO learner_svc.rejections
		  (article_id, market, reason, logged_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (article_id) DO UPDATE SET reason = EXCLUDED.reason, logged_at = now()
	`, articleID, market, reason)
	return err
}

// SourceExists checks deduplication by URL to avoid re-scraping unchanged content.
func SourceExists(ctx context.Context, pool *pgxpool.Pool, url string, maxAge time.Duration) (bool, error) {
	var fetched time.Time
	err := pool.QueryRow(ctx, `SELECT fetched_at FROM learner_svc.sources WHERE url = $1`, url).Scan(&fetched)
	if err != nil {
		return false, nil // not found → should scrape
	}
	return time.Since(fetched) < maxAge, nil
}

// TopicSuggestionRow mirrors learner_svc.topic_suggestions for DB I/O.
type TopicSuggestionRow struct {
	TopicID     string
	TopicName   string
	SourceCount int
	ExampleURLs []string
}

// UpsertTopicSuggestion writes or refreshes a topic suggestion for a market.
func UpsertTopicSuggestion(ctx context.Context, pool *pgxpool.Pool, market string, s TopicSuggestionRow) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO learner_svc.topic_suggestions
		  (market, topic_id, topic_name, source_count, example_urls, suggested_at)
		VALUES ($1, $2, $3, $4, $5, now())
		ON CONFLICT (market, topic_id) DO UPDATE
		  SET topic_name   = EXCLUDED.topic_name,
		      source_count = EXCLUDED.source_count,
		      example_urls = EXCLUDED.example_urls,
		      suggested_at = now()
	`, market, s.TopicID, s.TopicName, s.SourceCount, s.ExampleURLs)
	return err
}

// GetTopicSuggestions returns the most recently updated suggestions for a market.
func GetTopicSuggestions(ctx context.Context, pool *pgxpool.Pool, market string, limit int) ([]TopicSuggestionRow, error) {
	rows, err := pool.Query(ctx, `
		SELECT topic_id, topic_name, source_count, example_urls
		FROM learner_svc.topic_suggestions
		WHERE market = $1
		ORDER BY suggested_at DESC, source_count DESC
		LIMIT $2
	`, market, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []TopicSuggestionRow
	for rows.Next() {
		var s TopicSuggestionRow
		if err := rows.Scan(&s.TopicID, &s.TopicName, &s.SourceCount, &s.ExampleURLs); err != nil {
			continue
		}
		results = append(results, s)
	}
	return results, rows.Err()
}

// RecentSourceTitles returns the titles and URLs of sources scraped in the last `days` days for a market.
func RecentSourceTitles(ctx context.Context, pool *pgxpool.Pool, market string, days int, limit int) (titles []string, urls []string, err error) {
	rows, err := pool.Query(ctx, `
		SELECT title, url
		FROM learner_svc.sources
		WHERE market = $1
		  AND fetched_at > now() - ($2 || ' days')::interval
		  AND title IS NOT NULL AND title != ''
		ORDER BY fetched_at DESC
		LIMIT $3
	`, market, days, limit)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var title, url string
		if err := rows.Scan(&title, &url); err != nil {
			continue
		}
		titles = append(titles, title)
		urls = append(urls, url)
	}
	return titles, urls, rows.Err()
}
