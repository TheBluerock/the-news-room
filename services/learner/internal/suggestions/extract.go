package suggestions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// openAIChatURL is a var (not const) so tests can swap it for an httptest server.
var openAIChatURL = "https://api.openai.com/v1/chat/completions"

// TopicSuggestion is one extracted editorial topic.
type TopicSuggestion struct {
	TopicID     string   `json:"topic_id"`
	TopicName   string   `json:"topic_name"`
	SourceCount int      `json:"source_count"`
	ExampleURLs []string `json:"example_urls"`
}

// ExtractFromTitles calls OpenAI once per market scrape to distil recent
// source headlines into actionable editorial topic suggestions.
func ExtractFromTitles(ctx context.Context, apiKey, market string, titles []string, exampleURLs []string, limit int) ([]TopicSuggestion, error) {
	if len(titles) == 0 {
		return nil, nil
	}

	// Cap input to avoid token waste
	if len(titles) > 40 {
		titles = titles[:40]
	}

	wrappedPrompt := fmt.Sprintf(
		`You are an editorial analyst for %s wine and food journalism.
Given these recent headlines from industry sources, identify the top %d topics most worth covering in an article this week.
Respond ONLY with valid JSON: {"suggestions":[{"topic_id":"slug","topic_name":"Full Name","source_count":<int>}]}
topic_id must be lowercase, hyphen-separated. source_count is how many headlines relate to that topic.

Headlines:
%s`,
		market, limit, strings.Join(titles, "\n"),
	)

	body, _ := json.Marshal(map[string]interface{}{
		"model": "gpt-4o",
		"messages": []map[string]string{
			{"role": "user", "content": wrappedPrompt},
		},
		"temperature":     0,
		"response_format": map[string]string{"type": "json_object"},
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		openAIChatURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var apiResp struct {
		Choices []struct {
			Message struct{ Content string `json:"content"` } `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}
	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("empty OpenAI response")
	}

	var result struct {
		Suggestions []TopicSuggestion `json:"suggestions"`
	}
	if err := json.Unmarshal([]byte(apiResp.Choices[0].Message.Content), &result); err != nil {
		return nil, fmt.Errorf("parse suggestions: %w", err)
	}

	// Attach example URLs (up to 3 per suggestion)
	urls := exampleURLs
	if len(urls) > 3 {
		urls = urls[:3]
	}
	for i := range result.Suggestions {
		result.Suggestions[i].ExampleURLs = urls
	}

	return result.Suggestions, nil
}
