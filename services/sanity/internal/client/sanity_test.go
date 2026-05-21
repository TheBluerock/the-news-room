package client

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func withMockBase(t *testing.T, srv *httptest.Server) {
	t.Helper()
	orig := apiBaseURLTemplate
	// httptest server serves on http://127.0.0.1:NNN — strip subdomain template variable.
	apiBaseURLTemplate = strings.TrimSuffix(srv.URL, "/") + "%.0s"
	t.Cleanup(func() {
		apiBaseURLTemplate = orig
		srv.Close()
	})
}

func TestNewSlug(t *testing.T) {
	s := NewSlug("hello-world")
	if s.Type != "slug" || s.Current != "hello-world" {
		t.Errorf("slug = %+v", s)
	}
}

func TestNew(t *testing.T) {
	c := New("proj", "production", "token")
	if c.projectID != "proj" || c.dataset != "production" || c.token != "token" {
		t.Errorf("client = %+v", c)
	}
	if c.http == nil {
		t.Error("http client nil")
	}
}

func TestCreateDraft_HappyPath(t *testing.T) {
	var capturedBody []byte
	var capturedPath, capturedAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		capturedPath = r.URL.Path
		capturedBody, _ = io.ReadAll(r.Body)
		_, _ = io.WriteString(w, `{"results":[{}]}`)
	}))
	withMockBase(t, srv)

	c := New("proj", "production", "my-token")
	err := c.CreateDraft(context.Background(), ArticleDoc{
		ArticleID: "art-1",
		Market:    "italy",
		Language:  "it",
		Content:   "body",
		Title:     "T",
	})
	if err != nil {
		t.Fatalf("CreateDraft: %v", err)
	}
	if capturedAuth != "Bearer my-token" {
		t.Errorf("auth = %q", capturedAuth)
	}
	if !strings.Contains(capturedPath, "/data/mutate/production") {
		t.Errorf("path = %q", capturedPath)
	}

	// Mutation must wrap doc in createOrReplace, ID prefixed with "drafts."
	var payload map[string]any
	_ = json.Unmarshal(capturedBody, &payload)
	muts := payload["mutations"].([]any)
	if len(muts) != 1 {
		t.Fatalf("mutations len = %d", len(muts))
	}
	op := muts[0].(map[string]any)["createOrReplace"].(map[string]any)
	if op["_id"] != "drafts.art-1" {
		t.Errorf("_id = %v", op["_id"])
	}
	if op["_type"] != "article" {
		t.Errorf("_type = %v", op["_type"])
	}
}

func TestCreateDraft_Non2xxReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"bad request"}`)
	}))
	withMockBase(t, srv)

	c := New("proj", "production", "tok")
	err := c.CreateDraft(context.Background(), ArticleDoc{ArticleID: "art-1"})
	if err == nil {
		t.Fatal("expected error on 400")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("err missing status: %v", err)
	}
}

func TestCreateDraft_NetworkError(t *testing.T) {
	// Dead port → connection refused.
	orig := apiBaseURLTemplate
	apiBaseURLTemplate = "http://127.0.0.1:1%.0s"
	t.Cleanup(func() { apiBaseURLTemplate = orig })

	c := New("proj", "production", "tok")
	if err := c.CreateDraft(context.Background(), ArticleDoc{ArticleID: "x"}); err == nil {
		t.Fatal("expected network error")
	}
}

func TestCreateDraft_BadURL(t *testing.T) {
	orig := apiBaseURLTemplate
	apiBaseURLTemplate = "http://\x00bad%.0s"
	t.Cleanup(func() { apiBaseURLTemplate = orig })

	c := New("proj", "production", "tok")
	if err := c.CreateDraft(context.Background(), ArticleDoc{ArticleID: "x"}); err == nil {
		t.Fatal("expected request-build error")
	}
}

// ── webhook signature verification ───────────────────────────────────────────

func mkSig(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func TestVerifyWebhookSignature_Valid(t *testing.T) {
	body := []byte(`{"k":"v"}`)
	ts := "1700000000"
	sig := mkSig("secret", ts, body)
	header := fmt.Sprintf("t=%s,v1=%s", ts, sig)

	if !VerifyWebhookSignature("secret", header, body) {
		t.Error("valid signature rejected")
	}
}

func TestVerifyWebhookSignature_WrongSecret(t *testing.T) {
	body := []byte(`{"k":"v"}`)
	ts := "1700000000"
	sig := mkSig("right-secret", ts, body)
	header := fmt.Sprintf("t=%s,v1=%s", ts, sig)

	if VerifyWebhookSignature("WRONG", header, body) {
		t.Error("signature with wrong secret should fail")
	}
}

func TestVerifyWebhookSignature_TamperedBody(t *testing.T) {
	original := []byte(`{"k":"v"}`)
	ts := "1700000000"
	sig := mkSig("secret", ts, original)
	header := fmt.Sprintf("t=%s,v1=%s", ts, sig)

	tampered := []byte(`{"k":"BAD"}`)
	if VerifyWebhookSignature("secret", header, tampered) {
		t.Error("tampered body should fail verification")
	}
}

func TestVerifyWebhookSignature_MissingTimestamp(t *testing.T) {
	if VerifyWebhookSignature("s", "v1=abc", []byte("body")) {
		t.Error("missing t= should fail")
	}
}

func TestVerifyWebhookSignature_MissingV1(t *testing.T) {
	if VerifyWebhookSignature("s", "t=123", []byte("body")) {
		t.Error("missing v1= should fail")
	}
}

func TestVerifyWebhookSignature_MalformedHeader(t *testing.T) {
	if VerifyWebhookSignature("s", "garbage,no-equals", []byte("body")) {
		t.Error("malformed header should fail")
	}
}

func TestVerifyWebhookSignature_EmptyHeader(t *testing.T) {
	if VerifyWebhookSignature("s", "", []byte("body")) {
		t.Error("empty header should fail")
	}
}

func TestVerifyWebhookSignature_HeaderWithExtraFields(t *testing.T) {
	// Real Sanity headers can include extra fields — must still verify.
	body := []byte(`{"k":"v"}`)
	ts := "1700000000"
	sig := mkSig("secret", ts, body)
	header := fmt.Sprintf("t=%s,v1=%s,v0=ignored", ts, sig)
	if !VerifyWebhookSignature("secret", header, body) {
		t.Error("extra header field should not break verification")
	}
}
