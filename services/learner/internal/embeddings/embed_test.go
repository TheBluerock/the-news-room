package embeddings

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func withMockURL(t *testing.T, srv *httptest.Server) {
	t.Helper()
	orig := openAIEmbeddingsURL
	openAIEmbeddingsURL = srv.URL
	t.Cleanup(func() {
		openAIEmbeddingsURL = orig
		srv.Close()
	})
}

func TestGenerate_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("missing/bad auth header: %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("missing JSON content type")
		}

		var req embeddingRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Input != "hello world" || req.Model != embeddingModel {
			t.Errorf("unexpected req: %+v", req)
		}

		vec := make([]float32, Dimension)
		for i := range vec {
			vec[i] = float32(i) / float32(Dimension)
		}
		_ = json.NewEncoder(w).Encode(embeddingResponse{
			Data: []struct {
				Embedding []float32 `json:"embedding"`
			}{{Embedding: vec}},
		})
	}))
	withMockURL(t, srv)

	vec, err := Generate(context.Background(), "test-key", "hello world")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(vec) != Dimension {
		t.Errorf("len = %d, want %d", len(vec), Dimension)
	}
	if vec[0] != 0 || vec[1535] == 0 {
		t.Errorf("unexpected vector contents")
	}
}

func TestGenerate_Non200StatusReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "no key")
	}))
	withMockURL(t, srv)

	_, err := Generate(context.Background(), "bad", "x")
	if err == nil {
		t.Fatal("expected error on 401")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should mention status: %v", err)
	}
}

func TestGenerate_EmptyDataReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data": []}`)
	}))
	withMockURL(t, srv)

	_, err := Generate(context.Background(), "k", "x")
	if err == nil {
		t.Fatal("expected error on empty data")
	}
	if !strings.Contains(err.Error(), "empty embedding") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestGenerate_MalformedJSONReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "{not json")
	}))
	withMockURL(t, srv)

	if _, err := Generate(context.Background(), "k", "x"); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestGenerate_RequestBuildFailsOnBadURL(t *testing.T) {
	// Set a URL that fails URL parsing (control char).
	orig := openAIEmbeddingsURL
	openAIEmbeddingsURL = "http://\x00bad-url"
	t.Cleanup(func() { openAIEmbeddingsURL = orig })

	if _, err := Generate(context.Background(), "k", "x"); err == nil {
		t.Fatal("expected request-build error on malformed URL")
	}
}

func TestGenerate_NetworkErrorReturnsErr(t *testing.T) {
	// Set URL to a dead port — connection should fail immediately.
	orig := openAIEmbeddingsURL
	openAIEmbeddingsURL = "http://127.0.0.1:1"
	t.Cleanup(func() { openAIEmbeddingsURL = orig })

	if _, err := Generate(context.Background(), "k", "x"); err == nil {
		t.Fatal("expected network error")
	}
}

func TestGenerate_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block forever to ensure context cancellation surfaces.
		<-r.Context().Done()
	}))
	withMockURL(t, srv)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Generate(ctx, "k", "x"); err == nil {
		t.Fatal("expected ctx cancel error")
	}
}

func TestDimensionConstant(t *testing.T) {
	if Dimension != 1536 {
		t.Errorf("Dimension = %d, want 1536 (ada-002 contract)", Dimension)
	}
}
