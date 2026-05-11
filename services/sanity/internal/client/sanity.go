package client

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const apiVersion = "v2021-06-07"

type Client struct {
	projectID string
	dataset   string
	token     string
	http      *http.Client
}

func New(projectID, dataset, token string) *Client {
	return &Client{
		projectID: projectID,
		dataset:   dataset,
		token:     token,
		http:      &http.Client{},
	}
}

type ArticleDoc struct {
	ID           string  `json:"_id"`
	Type         string  `json:"_type"`
	ArticleID    string  `json:"articleId"`
	Market       string  `json:"market"`
	Language     string  `json:"language"`
	Content      string  `json:"content"`
	QualityScore float64 `json:"qualityScore"`
	ApprovedAt   string  `json:"approvedAt"`
}

// CreateDraft upserts a Sanity draft document for the article.
// Uses createOrReplace so DLQ replay is idempotent — same _id, same data.
func (c *Client) CreateDraft(ctx context.Context, doc ArticleDoc) error {
	doc.ID = "drafts." + doc.ArticleID
	doc.Type = "article"

	mutation := map[string]interface{}{
		"mutations": []map[string]interface{}{
			{"createOrReplace": doc},
		},
	}
	body, err := json.Marshal(mutation)
	if err != nil {
		return fmt.Errorf("marshal mutation: %w", err)
	}

	url := fmt.Sprintf("https://%s.api.sanity.io/%s/data/mutate/%s",
		c.projectID, apiVersion, c.dataset)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("sanity API request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var errBody map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		return fmt.Errorf("sanity API HTTP %d: %v", resp.StatusCode, errBody)
	}
	return nil
}

// VerifyWebhookSignature validates the sanity-webhook-signature header.
// Header format: t=<unix_timestamp>,v1=<base64_hmac_sha256>
// HMAC input: <timestamp>.<raw_body>
func VerifyWebhookSignature(secret, sigHeader string, body []byte) bool {
	var timestamp, sig string
	for _, part := range strings.Split(sigHeader, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "t":
			timestamp = kv[1]
		case "v1":
			sig = kv[1]
		}
	}
	if timestamp == "" || sig == "" {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(expected), []byte(sig))
}
