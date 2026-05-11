package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/newsroom/sanity/internal/client"
)

const topicPublished = "article.published"

type publishedEvent struct {
	EventID   string `json:"event_id"`
	TraceID   string `json:"trace_id"`
	ArticleID string `json:"article_id"`
	Market    string `json:"market"`
	SanityID  string `json:"sanity_id"`
	Timestamp string `json:"timestamp"`
}

// Server handles incoming Sanity publish webhooks and produces article.published events.
type Server struct {
	webhookSecret string
	producer      *kgo.Client
	logger        *slog.Logger
}

func NewServer(brokers []string, webhookSecret string, logger *slog.Logger) (*Server, error) {
	cl, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	if err != nil {
		return nil, fmt.Errorf("kafka producer: %w", err)
	}
	return &Server{
		webhookSecret: webhookSecret,
		producer:      cl,
		logger:        logger,
	}, nil
}

func (s *Server) Close() { s.producer.Close() }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhook/sanity", s.handleWebhook)
	return mux
}

func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		s.logger.Error("read webhook body", "err", err)
		http.Error(w, "read error", http.StatusInternalServerError)
		return
	}

	sigHeader := r.Header.Get("sanity-webhook-signature")
	if sigHeader == "" || !client.VerifyWebhookSignature(s.webhookSecret, sigHeader, body) {
		s.logger.Warn("webhook signature invalid", "remote_addr", r.RemoteAddr)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var doc struct {
		SanityID  string `json:"_id"`
		ArticleID string `json:"articleId"`
		Market    string `json:"market"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		s.logger.Error("unmarshal webhook body", "err", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if doc.ArticleID == "" || doc.Market == "" {
		s.logger.Warn("webhook missing articleId or market", "sanity_id", doc.SanityID)
		http.Error(w, "missing required fields", http.StatusBadRequest)
		return
	}

	traceCtx, span := otel.Tracer("sanity/webhook").Start(r.Context(), "PublishArticle")
	defer span.End()

	carrier := make(propagation.MapCarrier)
	otel.GetTextMapPropagator().Inject(traceCtx, carrier)

	evt := publishedEvent{
		EventID:   uuid.New().String(),
		TraceID:   carrier["traceparent"],
		ArticleID: doc.ArticleID,
		Market:    doc.Market,
		SanityID:  doc.SanityID,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.Marshal(evt)
	if err != nil {
		s.logger.Error("marshal article.published", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	var headers []kgo.RecordHeader
	for k, v := range carrier {
		headers = append(headers, kgo.RecordHeader{Key: k, Value: []byte(v)})
	}

	res := s.producer.ProduceSync(context.Background(), &kgo.Record{
		Topic:   topicPublished,
		Key:     []byte(doc.ArticleID),
		Value:   data,
		Headers: headers,
	})
	if err := res[0].Err; err != nil {
		s.logger.Error("produce article.published", "article_id", doc.ArticleID, "err", err)
		http.Error(w, "produce error", http.StatusInternalServerError)
		return
	}

	s.logger.Info("article.published emitted", "article_id", doc.ArticleID, "sanity_id", doc.SanityID, "market", doc.Market)
	w.WriteHeader(http.StatusOK)
}
