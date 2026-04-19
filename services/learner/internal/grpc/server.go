package grpcserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	learnerv1 "github.com/newsroom/proto/learner/v1"

	"github.com/newsroom/learner/internal/db"
	"github.com/newsroom/learner/internal/graph"
)

type Server struct {
	learnerv1.UnimplementedLearnerServiceServer
	pool      *pgxpool.Pool
	rdb       *redis.Client
	openAIKey string
}

func New(pool *pgxpool.Pool, rdb *redis.Client, openAIKey string) *Server {
	return &Server{pool: pool, rdb: rdb, openAIKey: openAIKey}
}

func (s *Server) QueryKnowledgeGraph(ctx context.Context, req *learnerv1.QueryRequest) (*learnerv1.QueryResponse, error) {
	if req.Market == "" || req.Query == "" {
		return nil, status.Error(codes.InvalidArgument, "market and query are required")
	}

	limit := int(req.Limit)
	if limit <= 0 || limit > 50 {
		limit = 10
	}

	nodes, err := graph.QueryKnowledgeGraph(ctx, s.pool, s.rdb, req.Market, req.Query, limit, s.openAIKey)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "knowledge graph query: %v", err)
	}

	resp := &learnerv1.QueryResponse{
		Nodes: make([]*learnerv1.KnowledgeNode, len(nodes)),
	}
	for i, n := range nodes {
		resp.Nodes[i] = &learnerv1.KnowledgeNode{
			Id:      n.ID,
			Type:    n.Type,
			Content: n.Content,
			Weight:  n.Weight,
		}
	}
	return resp, nil
}

func (s *Server) GetJournalistProfile(ctx context.Context, req *learnerv1.ProfileRequest) (*learnerv1.ProfileResponse, error) {
	if req.JournalistId == "" {
		return nil, status.Error(codes.InvalidArgument, "journalist_id is required")
	}

	p, err := db.GetJournalistProfile(ctx, s.pool, req.JournalistId)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "journalist %s: %v", req.JournalistId, err)
	}

	return &learnerv1.ProfileResponse{
		JournalistId:   p.JournalistID,
		Market:         p.Market,
		Specialization: p.Specialization,
		StyleProfile:   p.StyleProfile,
	}, nil
}

func (s *Server) GetTopicSuggestions(ctx context.Context, req *learnerv1.SuggestionsRequest) (*learnerv1.SuggestionsResponse, error) {
	if req.Market == "" {
		return nil, status.Error(codes.InvalidArgument, "market is required")
	}
	limit := int(req.Limit)
	if limit <= 0 || limit > 50 {
		limit = 10
	}

	rows, err := db.GetTopicSuggestions(ctx, s.pool, req.Market, limit)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "get suggestions: %v", err)
	}

	resp := &learnerv1.SuggestionsResponse{
		Suggestions: make([]*learnerv1.TopicSuggestion, len(rows)),
	}
	for i, r := range rows {
		resp.Suggestions[i] = &learnerv1.TopicSuggestion{
			TopicId:     r.TopicID,
			TopicName:   r.TopicName,
			SourceCount: int32(r.SourceCount),
			ExampleUrls: r.ExampleURLs,
		}
	}
	return resp, nil
}

func (s *Server) ScoreFactualAccuracy(ctx context.Context, req *learnerv1.FactualRequest) (*learnerv1.FactualResponse, error) {
	if req.Market == "" || req.Content == "" {
		return nil, status.Error(codes.InvalidArgument, "market and content are required")
	}

	score, issues, err := scoreFactual(ctx, s.openAIKey, req.Market, req.Content)
	if err != nil {
		// Don't fail the request on LLM error; return a neutral score with a note
		return &learnerv1.FactualResponse{
			AccuracyScore: 0.5,
			Issues:        []string{fmt.Sprintf("scoring unavailable: %v", err)},
		}, nil
	}

	return &learnerv1.FactualResponse{
		AccuracyScore: score,
		Issues:        issues,
	}, nil
}

// scoreFactual calls OpenAI GPT-4o to assess factual accuracy.
func scoreFactual(ctx context.Context, apiKey, market, content string) (float32, []string, error) {
	prompt := fmt.Sprintf(`You are a fact-checker for %s wine and food journalism.
Assess the factual accuracy of the following article excerpt.
Respond ONLY with valid JSON: {"accuracy_score": <0.0-1.0>, "issues": ["issue1", "issue2"]}
A score of 1.0 means no factual issues found.

Article:
%s`, market, content)

	reqBody, _ := json.Marshal(map[string]interface{}{
		"model": "gpt-4o",
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"temperature":     0,
		"response_format": map[string]string{"type": "json_object"},
	})

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.openai.com/v1/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return 0, nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	var apiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return 0, nil, err
	}
	if len(apiResp.Choices) == 0 {
		return 0, nil, fmt.Errorf("empty response from OpenAI")
	}

	var result struct {
		AccuracyScore float32  `json:"accuracy_score"`
		Issues        []string `json:"issues"`
	}
	if err := json.Unmarshal([]byte(apiResp.Choices[0].Message.Content), &result); err != nil {
		return 0, nil, fmt.Errorf("parse factual response: %w", err)
	}
	return result.AccuracyScore, result.Issues, nil
}
