package adminhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ── mock DB ──────────────────────────────────────────────────────────────────

type mockDB struct {
	queryFn    func(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	queryRowFn func(ctx context.Context, sql string, args ...any) pgx.Row
	execFn     func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func (m *mockDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return m.queryFn(ctx, sql, args...)
}
func (m *mockDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return m.queryRowFn(ctx, sql, args...)
}
func (m *mockDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return m.execFn(ctx, sql, args...)
}

// emptyRows satisfies pgx.Rows and returns no results.
type emptyRows struct{}

func (emptyRows) Close()                        {}
func (emptyRows) Err() error                    { return nil }
func (emptyRows) CommandTag() pgconn.CommandTag { return pgconn.CommandTag{} }
func (emptyRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (emptyRows) Next() bool                    { return false }
func (emptyRows) Scan(...any) error             { return nil }
func (emptyRows) Values() ([]any, error)        { return nil, nil }
func (emptyRows) RawValues() [][]byte           { return nil }
func (emptyRows) Conn() *pgx.Conn               { return nil }

// errRow is a pgx.Row that always returns an error on Scan.
type errRow struct{ err error }

func (e errRow) Scan(...any) error { return e.err }

// scalarRow scans a single integer value.
type scalarRow struct{ vals []any }

func (r *scalarRow) Scan(dest ...any) error {
	for i, d := range dest {
		switch v := d.(type) {
		case *int:
			*v = r.vals[i].(int)
		case *float64:
			*v = r.vals[i].(float64)
		}
	}
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func newServer(db querier) *http.ServeMux {
	s := &Server{db: db, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/calendar/{market}", s.listCalendar)
	mux.HandleFunc("POST /api/calendar/{market}", s.createCalendar)
	mux.HandleFunc("DELETE /api/calendar/{market}/{id}", s.deleteCalendar)
	mux.HandleFunc("GET /api/analytics/market/{market}", s.getMarketAnalytics)
	return mux
}

func do(t *testing.T, mux *http.ServeMux, method, path, body, role string) *httptest.ResponseRecorder {
	t.Helper()
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, bodyReader)
	if role != "" {
		req.Header.Set("X-User-Role", role)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

// ── calendar tests ────────────────────────────────────────────────────────────

func TestListCalendar_InvalidMarket(t *testing.T) {
	mux := newServer(&mockDB{})
	w := do(t, mux, "GET", "/api/calendar/invalid", "", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", w.Code)
	}
}

func TestListCalendar_EmptyResult(t *testing.T) {
	db := &mockDB{
		queryFn: func(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
			return emptyRows{}, nil
		},
	}
	mux := newServer(db)
	w := do(t, mux, "GET", "/api/calendar/italy", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d", w.Code)
	}
	var result []any
	json.NewDecoder(w.Body).Decode(&result)
	if len(result) != 0 {
		t.Fatalf("want empty array got %v", result)
	}
}

func TestCreateCalendar_Forbidden(t *testing.T) {
	mux := newServer(&mockDB{})
	w := do(t, mux, "POST", "/api/calendar/italy", `{"topic_name":"t","scheduled_at":"2026-06-01T00:00:00Z"}`, "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403 got %d", w.Code)
	}
}

func TestCreateCalendar_MissingFields(t *testing.T) {
	mux := newServer(&mockDB{})
	w := do(t, mux, "POST", "/api/calendar/italy", `{}`, "admin")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", w.Code)
	}
}

func TestCreateCalendar_BadScheduledAt(t *testing.T) {
	mux := newServer(&mockDB{})
	body := `{"topic_name":"Barolo","scheduled_at":"not-a-date"}`
	w := do(t, mux, "POST", "/api/calendar/italy", body, "admin")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", w.Code)
	}
}

func TestCreateCalendar_OK(t *testing.T) {
	created := false
	db2 := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...any) pgx.Row {
			created = true
			return &stringRow{val: "abc-123"}
		},
	}
	mux := newServer(db2)
	body := `{"topic_name":"Barolo","scheduled_at":"2026-06-01T00:00:00Z"}`
	w := do(t, mux, "POST", "/api/calendar/italy", body, "admin")
	if w.Code != http.StatusCreated {
		t.Fatalf("want 201 got %d body=%s", w.Code, w.Body.String())
	}
	if !created {
		t.Fatal("DB not called")
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["id"] == "" {
		t.Fatal("response missing id")
	}
}

// stringRow scans a single string.
type stringRow struct{ val string }

func (r *stringRow) Scan(dest ...any) error {
	if s, ok := dest[0].(*string); ok {
		*s = r.val
	}
	return nil
}

func TestDeleteCalendar_Forbidden(t *testing.T) {
	mux := newServer(&mockDB{})
	w := do(t, mux, "DELETE", "/api/calendar/italy/some-id", "", "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403 got %d", w.Code)
	}
}

func TestDeleteCalendar_NotFound(t *testing.T) {
	db := &mockDB{
		execFn: func(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("DELETE 0"), nil
		},
	}
	mux := newServer(db)
	w := do(t, mux, "DELETE", "/api/calendar/italy/missing-id", "", "admin")
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 got %d", w.Code)
	}
}

func TestDeleteCalendar_OK(t *testing.T) {
	db := &mockDB{
		execFn: func(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("DELETE 1"), nil
		},
	}
	mux := newServer(db)
	w := do(t, mux, "DELETE", "/api/calendar/italy/some-id", "", "admin")
	if w.Code != http.StatusNoContent {
		t.Fatalf("want 204 got %d", w.Code)
	}
}

// ── analytics tests ───────────────────────────────────────────────────────────

func TestGetMarketAnalytics_InvalidMarket(t *testing.T) {
	mux := newServer(&mockDB{})
	w := do(t, mux, "GET", "/api/analytics/market/invalid", "", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", w.Code)
	}
}

func TestGetMarketAnalytics_OK(t *testing.T) {
	callN := 0
	db := &mockDB{
		queryRowFn: func(_ context.Context, _ string, _ ...any) pgx.Row {
			callN++
			switch callN {
			case 1: // article count + avg quality
				return &twoIntFloatRow{i: 42, f: 0.85}
			case 2: // pending queue
				return &scalarRow{vals: []any{7}}
			}
			return errRow{err: pgx.ErrNoRows}
		},
		queryFn: func(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
			return emptyRows{}, nil // top rejection reasons — empty
		},
	}
	mux := newServer(db)
	w := do(t, mux, "GET", "/api/analytics/market/italy", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200 got %d body=%s", w.Code, w.Body.String())
	}
	var result marketAnalytics
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Market != "italy" {
		t.Fatalf("want market=italy got %s", result.Market)
	}
	if result.ArticleCount30d != 42 {
		t.Fatalf("want article_count=42 got %d", result.ArticleCount30d)
	}
	if result.AvgQualityScore < 0.84 || result.AvgQualityScore > 0.86 {
		t.Fatalf("want avg_quality≈0.85 got %f", result.AvgQualityScore)
	}
}

// twoIntFloatRow scans (int, float64).
type twoIntFloatRow struct{ i int; f float64 }

func (r *twoIntFloatRow) Scan(dest ...any) error {
	if len(dest) < 2 {
		return nil
	}
	if v, ok := dest[0].(*int); ok {
		*v = r.i
	}
	if v, ok := dest[1].(*float64); ok {
		*v = r.f
	}
	return nil
}

// ── JSON body helper ──────────────────────────────────────────────────────────

func jsonBody(v any) io.Reader {
	b, _ := json.Marshal(v)
	return bytes.NewReader(b)
}
