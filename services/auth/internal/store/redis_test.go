package store

import (
	"context"
	"sync"
	"testing"
	"time"

	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

func startRedis(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	c, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		t.Skipf("testcontainers/redis unavailable: %v", err)
	}
	t.Cleanup(func() {
		_ = c.Terminate(context.Background())
	})
	host, err := c.Host(ctx)
	if err != nil {
		t.Fatalf("redis host: %v", err)
	}
	port, err := c.MappedPort(ctx, "6379/tcp")
	if err != nil {
		t.Fatalf("redis port: %v", err)
	}
	return host + ":" + port.Port()
}

func TestConnectRedis_OK(t *testing.T) {
	addr := startRedis(t)
	rdb, err := ConnectRedis(context.Background(), addr)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer rdb.Close()
}

func TestConnectRedis_BadAddr(t *testing.T) {
	_, err := ConnectRedis(context.Background(), "127.0.0.1:1")
	if err == nil {
		t.Fatal("expected ping error on dead addr")
	}
}

func TestBlock_IsBlocked_RoundTrip(t *testing.T) {
	addr := startRedis(t)
	rdb, _ := ConnectRedis(context.Background(), addr)
	defer rdb.Close()
	ctx := context.Background()

	jti := "jti-1"
	blocked, err := IsBlocked(ctx, rdb, jti)
	if err != nil {
		t.Fatalf("IsBlocked: %v", err)
	}
	if blocked {
		t.Error("jti should not be blocked initially")
	}
	if err := Block(ctx, rdb, jti, time.Minute); err != nil {
		t.Fatalf("Block: %v", err)
	}
	blocked, err = IsBlocked(ctx, rdb, jti)
	if err != nil {
		t.Fatalf("IsBlocked: %v", err)
	}
	if !blocked {
		t.Error("jti should be blocked after Block")
	}
}

func TestBlock_TTLExpires(t *testing.T) {
	addr := startRedis(t)
	rdb, _ := ConnectRedis(context.Background(), addr)
	defer rdb.Close()
	ctx := context.Background()

	if err := Block(ctx, rdb, "short", 500*time.Millisecond); err != nil {
		t.Fatalf("Block: %v", err)
	}
	if b, _ := IsBlocked(ctx, rdb, "short"); !b {
		t.Fatal("should be blocked immediately")
	}
	time.Sleep(900 * time.Millisecond)
	if b, _ := IsBlocked(ctx, rdb, "short"); b {
		t.Fatal("should expire after TTL")
	}
}

func TestBlock_Concurrent(t *testing.T) {
	addr := startRedis(t)
	rdb, _ := ConnectRedis(context.Background(), addr)
	defer rdb.Close()
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = Block(ctx, rdb, "shared-jti", time.Minute)
		}()
	}
	wg.Wait()
	if b, _ := IsBlocked(ctx, rdb, "shared-jti"); !b {
		t.Error("concurrent Block did not result in blocked state")
	}
}

func TestIsBlocked_DifferentJTIs(t *testing.T) {
	addr := startRedis(t)
	rdb, _ := ConnectRedis(context.Background(), addr)
	defer rdb.Close()
	ctx := context.Background()

	_ = Block(ctx, rdb, "a", time.Minute)
	if b, _ := IsBlocked(ctx, rdb, "b"); b {
		t.Error("unrelated jti should not be blocked")
	}
}
