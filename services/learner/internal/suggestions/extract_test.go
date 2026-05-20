package suggestions

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
	orig := openAIChatURL
	openAIChatURL = srv.URL
	t.Cleanup(func() {
		openAIChatURL = orig
		srv.Close()
	})
}

// openAIResponse simulates what /v1/chat/completions returns when given the
// suggestions prompt: choices[0].message.content is a JSON string that itself
// decodes into {"suggestions":[...]}.
func openAIResponse(suggestions []TopicSuggestion) string {
	inner, _ := json.Marshal(map[string]interface{}{"suggestions": suggestions})
	resp := map[string]interface{}{
		"choices": []map[string]interface{}{
			{"message": map[string]string{"content": string(inner)}},
		},
	}
	out, _ := json.Marshal(resp)
	return string(out)
}

func TestExtractFromTitles_EmptyTitlesReturnsNil(t *testing.T) {
	got, err := ExtractFromTitles(context.Background(), "k", "italy", nil, nil, 5)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestExtractFromTitles_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer my-key" {
			t.Errorf("bad auth: %q", r.Header.Get("Authorization"))
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "italy wine and food journalism") {
			t.Errorf("market not injected: %s", body)
		}
		if !strings.Contains(string(body), "top 3 topics") {
			t.Errorf("limit not injected: %s", body)
		}
		_, _ = io.WriteString(w, openAIResponse([]TopicSuggestion{
			{TopicID: "barolo-2018", TopicName: "Barolo 2018 vintage", SourceCount: 5},
			{TopicID: "nebbiolo", TopicName: "Nebbiolo trends", SourceCount: 3},
		}))
	}))
	withMockURL(t, srv)

	got, err := ExtractFromTitles(context.Background(), "my-key", "italy",
		[]string{"Barolo 2018 vintage notes", "Nebbiolo grape trends"},
		[]string{"http://a.com/1", "http://a.com/2"}, 3)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d suggestions, want 2", len(got))
	}
	if got[0].TopicID != "barolo-2018" || got[0].SourceCount != 5 {
		t.Errorf("first suggestion = %+v", got[0])
	}
	// Example URLs attached to every suggestion (max 3)
	for i, s := range got {
		if len(s.ExampleURLs) == 0 {
			t.Errorf("suggestion %d missing example urls", i)
		}
	}
}

func TestExtractFromTitles_TitlesCappedTo40(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		// Headlines block contains at most 40 title lines.
		count := strings.Count(string(body), "Title-")
		if count > 40 {
			t.Errorf("title count = %d, want ≤40", count)
		}
		_, _ = io.WriteString(w, openAIResponse(nil))
	}))
	withMockURL(t, srv)

	titles := make([]string, 100)
	for i := range titles {
		titles[i] = "Title-" + string(rune('A'+(i%26)))
	}
	_, _ = ExtractFromTitles(context.Background(), "k", "italy", titles, nil, 5)
}

func TestExtractFromTitles_ExampleURLsCappedTo3(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, openAIResponse([]TopicSuggestion{
			{TopicID: "a", TopicName: "A", SourceCount: 1},
		}))
	}))
	withMockURL(t, srv)

	urls := []string{"u1", "u2", "u3", "u4", "u5"}
	got, err := ExtractFromTitles(context.Background(), "k", "italy",
		[]string{"x"}, urls, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got[0].ExampleURLs) != 3 {
		t.Errorf("urls len = %d, want 3", len(got[0].ExampleURLs))
	}
}

func TestExtractFromTitles_NoChoicesReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[]}`)
	}))
	withMockURL(t, srv)

	_, err := ExtractFromTitles(context.Background(), "k", "italy", []string{"x"}, nil, 1)
	if err == nil {
		t.Fatal("expected error on empty choices")
	}
}

func TestExtractFromTitles_MalformedSuggestionsJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"not-json-at-all"}}]}`)
	}))
	withMockURL(t, srv)

	_, err := ExtractFromTitles(context.Background(), "k", "italy", []string{"x"}, nil, 1)
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestExtractFromTitles_MalformedOuterJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "{garbage")
	}))
	withMockURL(t, srv)

	if _, err := ExtractFromTitles(context.Background(), "k", "italy", []string{"x"}, nil, 1); err == nil {
		t.Fatal("expected outer decode error")
	}
}

func TestExtractFromTitles_NetworkError(t *testing.T) {
	orig := openAIChatURL
	openAIChatURL = "http://127.0.0.1:1"
	t.Cleanup(func() { openAIChatURL = orig })

	if _, err := ExtractFromTitles(context.Background(), "k", "italy", []string{"x"}, nil, 1); err == nil {
		t.Fatal("expected network error")
	}
}

func TestExtractFromTitles_PerMarketPromptShape(t *testing.T) {
	for _, market := range []string{"italy", "usa", "china"} {
		t.Run(market, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				if !strings.Contains(string(body), market+" wine and food journalism") {
					t.Errorf("market %q not in prompt: %s", market, body)
				}
				_, _ = io.WriteString(w, openAIResponse(nil))
			}))
			withMockURL(t, srv)
			_, _ = ExtractFromTitles(context.Background(), "k", market, []string{"x"}, nil, 1)
		})
	}
}
