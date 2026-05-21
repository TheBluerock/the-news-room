package webhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

func mkSig(secret, ts string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// newServerNoKafka constructs a Server without an actual producer.
// All tests below short-circuit before reaching ProduceSync so the nil producer
// is fine for status-code assertions.
func newServerNoKafka(secret string) *Server {
	return &Server{webhookSecret: secret, producer: nil, logger: quietLogger()}
}

func TestHandler_RegistersPostRoute(t *testing.T) {
	s := newServerNoKafka("secret")
	h := s.Handler()

	// Wrong method → 405 from ServeMux.
	req := httptest.NewRequest("GET", "/webhook/sanity", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET status = %d", rec.Code)
	}
}

func TestHandleWebhook_MissingSignatureRejected(t *testing.T) {
	s := newServerNoKafka("secret")
	req := httptest.NewRequest("POST", "/webhook/sanity", bytes.NewBufferString(`{}`))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestHandleWebhook_BadSignatureRejected(t *testing.T) {
	s := newServerNoKafka("secret")
	req := httptest.NewRequest("POST", "/webhook/sanity", bytes.NewBufferString(`{}`))
	req.Header.Set("sanity-webhook-signature", "t=1,v1=invalid")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestHandleWebhook_MalformedJSONRejected(t *testing.T) {
	s := newServerNoKafka("secret")
	body := []byte("{not json")
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := mkSig("secret", ts, body)

	req := httptest.NewRequest("POST", "/webhook/sanity", bytes.NewReader(body))
	req.Header.Set("sanity-webhook-signature", fmt.Sprintf("t=%s,v1=%s", ts, sig))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestHandleWebhook_MissingArticleIDRejected(t *testing.T) {
	s := newServerNoKafka("secret")
	body := []byte(`{"_id":"abc","market":"italy"}`) // articleId missing
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := mkSig("secret", ts, body)

	req := httptest.NewRequest("POST", "/webhook/sanity", bytes.NewReader(body))
	req.Header.Set("sanity-webhook-signature", fmt.Sprintf("t=%s,v1=%s", ts, sig))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestHandleWebhook_MissingMarketRejected(t *testing.T) {
	s := newServerNoKafka("secret")
	body := []byte(`{"_id":"abc","articleId":"a1"}`) // market missing
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := mkSig("secret", ts, body)

	req := httptest.NewRequest("POST", "/webhook/sanity", bytes.NewReader(body))
	req.Header.Set("sanity-webhook-signature", fmt.Sprintf("t=%s,v1=%s", ts, sig))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestClose_DoesNotPanic(t *testing.T) {
	// Use real kgo client so Close() has something to close.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Close panicked: %v", r)
		}
	}()
	s, err := NewServer([]string{"127.0.0.1:65531"}, "secret", quietLogger())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	s.Close()
}
