package graph

import (
	"context"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/newsroom/learner/internal/db"
	"github.com/newsroom/learner/internal/embeddings"
)

// QueryKnowledgeGraph runs combined vector + full-text search and returns
// the top `limit` most relevant knowledge nodes for the given market and query.
func QueryKnowledgeGraph(ctx context.Context, pool *pgxpool.Pool, _ *redis.Client, market, query string, limit int, openAIKey string) ([]db.KnowledgeNode, error) {
	// Generate query embedding
	vec, err := embeddings.Generate(ctx, openAIKey, query)
	if err != nil {
		// Fall back to full-text only if embedding fails
		return db.SearchFullText(ctx, pool, market, query, limit)
	}

	vecResults, err := db.SearchSimilar(ctx, pool, market, vec, limit)
	if err != nil {
		vecResults = nil
	}

	ftsResults, err := db.SearchFullText(ctx, pool, market, query, limit)
	if err != nil {
		ftsResults = nil
	}

	return mergeAndRank(vecResults, ftsResults, limit), nil
}

// mergeAndRank combines vector and FTS results, deduplicates by ID, and returns top N by weight.
func mergeAndRank(vec, fts []db.KnowledgeNode, limit int) []db.KnowledgeNode {
	seen := make(map[string]db.KnowledgeNode)
	for _, n := range vec {
		seen[n.ID] = n
	}
	for _, n := range fts {
		if existing, ok := seen[n.ID]; ok {
			// Boost score if found by both methods
			existing.Weight = (existing.Weight + n.Weight) / 2 * 1.2
			seen[n.ID] = existing
		} else {
			seen[n.ID] = n
		}
	}

	merged := make([]db.KnowledgeNode, 0, len(seen))
	for _, n := range seen {
		merged = append(merged, n)
	}
	sort.Slice(merged, func(i, j int) bool {
		return merged[i].Weight > merged[j].Weight
	})

	if len(merged) > limit {
		return merged[:limit]
	}
	return merged
}
