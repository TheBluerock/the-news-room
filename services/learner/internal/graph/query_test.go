package graph

import (
	"testing"

	"github.com/newsroom/learner/internal/db"
)

func node(id string, w float32) db.KnowledgeNode {
	return db.KnowledgeNode{ID: id, Type: "article", Weight: w}
}

func TestMergeAndRank_UniqueResultsSorted(t *testing.T) {
	vec := []db.KnowledgeNode{node("a", 0.3), node("b", 0.9)}
	fts := []db.KnowledgeNode{node("c", 0.5)}

	out := mergeAndRank(vec, fts, 10)
	if len(out) != 3 {
		t.Fatalf("got %d, want 3", len(out))
	}
	if out[0].ID != "b" {
		t.Errorf("top = %q, want b (highest weight)", out[0].ID)
	}
}

func TestMergeAndRank_DuplicateBoosted(t *testing.T) {
	vec := []db.KnowledgeNode{node("a", 0.4)}
	fts := []db.KnowledgeNode{node("a", 0.6)}

	out := mergeAndRank(vec, fts, 10)
	if len(out) != 1 {
		t.Fatalf("dedupe failed: %d entries", len(out))
	}
	// Boosted: ((0.4 + 0.6) / 2) * 1.2 = 0.6
	if out[0].Weight < 0.59 || out[0].Weight > 0.61 {
		t.Errorf("weight = %v, want ~0.6", out[0].Weight)
	}
}

func TestMergeAndRank_LimitApplied(t *testing.T) {
	vec := []db.KnowledgeNode{
		node("a", 0.1), node("b", 0.2), node("c", 0.3),
		node("d", 0.4), node("e", 0.5),
	}
	out := mergeAndRank(vec, nil, 3)
	if len(out) != 3 {
		t.Errorf("got %d, want 3", len(out))
	}
	// Top 3 by weight: e, d, c
	for i, want := range []string{"e", "d", "c"} {
		if out[i].ID != want {
			t.Errorf("out[%d] = %q, want %q", i, out[i].ID, want)
		}
	}
}

func TestMergeAndRank_BothEmpty(t *testing.T) {
	out := mergeAndRank(nil, nil, 5)
	if len(out) != 0 {
		t.Errorf("got %d, want 0", len(out))
	}
}

func TestMergeAndRank_LimitGreaterThanResults(t *testing.T) {
	vec := []db.KnowledgeNode{node("a", 0.1)}
	out := mergeAndRank(vec, nil, 100)
	if len(out) != 1 {
		t.Errorf("got %d, want 1", len(out))
	}
}

func TestMergeAndRank_StableForEqualWeights(t *testing.T) {
	// sort.Slice is not stable but we tolerate any tie-break order; just verify all are kept.
	vec := []db.KnowledgeNode{node("a", 0.5), node("b", 0.5), node("c", 0.5)}
	out := mergeAndRank(vec, nil, 10)
	if len(out) != 3 {
		t.Errorf("got %d, want 3", len(out))
	}
}
