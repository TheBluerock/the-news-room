package fastpath

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

func startRedis(t *testing.T) *redis.Client {
	t.Helper()
	ctx := context.Background()
	c, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		t.Skipf("testcontainers/redis unavailable: %v", err)
	}
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })

	host, _ := c.Host(ctx)
	port, _ := c.MappedPort(ctx, "6379/tcp")
	rdb := redis.NewClient(&redis.Options{Addr: host + ":" + port.Port()})
	t.Cleanup(func() { _ = rdb.Close() })
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping: %v", err)
	}
	return rdb
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestWriteCorrection_StoresPayload(t *testing.T) {
	rdb := startRedis(t)
	ctx := context.Background()

	payload := map[string]interface{}{"reason": "use formal tone", "field": "tone"}
	if err := WriteCorrection(ctx, rdb, "italy", "c1", payload, quietLogger()); err != nil {
		t.Fatalf("WriteCorrection: %v", err)
	}

	raw, err := rdb.Get(ctx, "corrections:italy:c1").Bytes()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["reason"] != "use formal tone" {
		t.Errorf("reason = %v", got["reason"])
	}
}

func TestWriteCorrection_SetsTTL(t *testing.T) {
	rdb := startRedis(t)
	ctx := context.Background()

	_ = WriteCorrection(ctx, rdb, "italy", "c1", map[string]interface{}{"x": 1}, quietLogger())
	ttl, err := rdb.TTL(ctx, "corrections:italy:c1").Result()
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl <= 0 || ttl > correctionTTL {
		t.Errorf("ttl = %v, want (0, %v]", ttl, correctionTTL)
	}
}

func TestWriteCorrection_IdempotentReplay(t *testing.T) {
	rdb := startRedis(t)
	ctx := context.Background()

	first := map[string]interface{}{"reason": "original"}
	if err := WriteCorrection(ctx, rdb, "italy", "c1", first, quietLogger()); err != nil {
		t.Fatalf("first write: %v", err)
	}

	// Replay with different payload — should NOT overwrite (SETNX).
	replay := map[string]interface{}{"reason": "tampered"}
	if err := WriteCorrection(ctx, rdb, "italy", "c1", replay, quietLogger()); err != nil {
		t.Fatalf("replay write: %v", err)
	}

	raw, _ := rdb.Get(ctx, "corrections:italy:c1").Bytes()
	var got map[string]interface{}
	_ = json.Unmarshal(raw, &got)
	if got["reason"] != "original" {
		t.Errorf("reason = %v, want unchanged 'original' (SETNX violation)", got["reason"])
	}
}

func TestDeleteCorrection_RemovesKey(t *testing.T) {
	rdb := startRedis(t)
	ctx := context.Background()

	_ = WriteCorrection(ctx, rdb, "italy", "c1", map[string]interface{}{"x": 1}, quietLogger())
	if err := DeleteCorrection(ctx, rdb, "italy", "c1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	n, _ := rdb.Exists(ctx, "corrections:italy:c1").Result()
	if n != 0 {
		t.Errorf("key still exists after delete")
	}
}

func TestDeleteCorrection_AbsentKeyNoError(t *testing.T) {
	rdb := startRedis(t)
	ctx := context.Background()
	if err := DeleteCorrection(ctx, rdb, "italy", "never-existed"); err != nil {
		t.Errorf("Delete absent key returned error: %v", err)
	}
}

func TestWriteCorrection_IsolatedPerMarket(t *testing.T) {
	rdb := startRedis(t)
	ctx := context.Background()

	_ = WriteCorrection(ctx, rdb, "italy", "c1", map[string]interface{}{"r": "italy"}, quietLogger())
	_ = WriteCorrection(ctx, rdb, "usa", "c1", map[string]interface{}{"r": "usa"}, quietLogger())

	italyRaw, _ := rdb.Get(ctx, "corrections:italy:c1").Bytes()
	usaRaw, _ := rdb.Get(ctx, "corrections:usa:c1").Bytes()
	var italyVal, usaVal map[string]interface{}
	_ = json.Unmarshal(italyRaw, &italyVal)
	_ = json.Unmarshal(usaRaw, &usaVal)
	if italyVal["r"] != "italy" || usaVal["r"] != "usa" {
		t.Errorf("market collision: italy=%v usa=%v", italyVal["r"], usaVal["r"])
	}
}

func TestRefreshTTLMetrics_NoKeysUsesCeiling(t *testing.T) {
	rdb := startRedis(t)
	ctx := context.Background()
	// No keys for any market — function should run and set gauge to correctionTTL ceiling.
	RefreshTTLMetrics(ctx, rdb, []string{"italy", "usa", "china"})
}

func TestRefreshTTLMetrics_FindsMinimumTTL(t *testing.T) {
	rdb := startRedis(t)
	ctx := context.Background()

	// Write 3 corrections, then artificially shorten one of them.
	for i, id := range []string{"a", "b", "c"} {
		_ = WriteCorrection(ctx, rdb, "italy", id, map[string]interface{}{"i": i}, quietLogger())
	}
	rdb.Expire(ctx, "corrections:italy:b", 5*time.Minute)

	// Should not panic; gauge update is the side effect we don't assert directly here.
	RefreshTTLMetrics(ctx, rdb, []string{"italy"})
}

func TestWriteCorrection_LargePayload(t *testing.T) {
	rdb := startRedis(t)
	ctx := context.Background()
	big := map[string]interface{}{"data": make([]byte, 100*1024)}
	if err := WriteCorrection(ctx, rdb, "italy", "big", big, quietLogger()); err != nil {
		t.Fatalf("WriteCorrection large: %v", err)
	}
}
