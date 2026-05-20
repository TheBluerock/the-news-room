package scraper

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeCfg(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "cfg.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

func TestLoadConfig_Valid(t *testing.T) {
	p := writeCfg(t, `
market: italy
scrape_interval: 30m
feeds:
  - url: https://example.com/feed.xml
    category: news
  - url: https://other.com/rss
    category: trends
`)
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Market != "italy" {
		t.Errorf("market = %q", cfg.Market)
	}
	if cfg.parsedInterval != 30*time.Minute {
		t.Errorf("parsedInterval = %v", cfg.parsedInterval)
	}
	if len(cfg.Feeds) != 2 {
		t.Errorf("feeds = %d, want 2", len(cfg.Feeds))
	}
	if cfg.Feeds[0].URL != "https://example.com/feed.xml" {
		t.Errorf("feed url = %q", cfg.Feeds[0].URL)
	}
}

func TestLoadConfig_InvalidDurationFallsBackTo6h(t *testing.T) {
	p := writeCfg(t, `
market: italy
scrape_interval: not-a-duration
feeds: []
`)
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.parsedInterval != 6*time.Hour {
		t.Errorf("parsedInterval = %v, want 6h fallback", cfg.parsedInterval)
	}
}

func TestLoadConfig_MissingFileReturnsError(t *testing.T) {
	if _, err := LoadConfig("/nonexistent/cfg.yaml"); err == nil {
		t.Fatal("expected file error")
	}
}

func TestLoadConfig_MalformedYAMLReturnsError(t *testing.T) {
	p := writeCfg(t, "this: is\n  not: \tvalid yaml [[[")
	if _, err := LoadConfig(p); err == nil {
		t.Fatal("expected yaml error")
	}
}

func TestLoadConfig_EmptyFeeds(t *testing.T) {
	p := writeCfg(t, `
market: usa
scrape_interval: 1h
`)
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Market != "usa" {
		t.Errorf("market = %q", cfg.Market)
	}
	if cfg.parsedInterval != time.Hour {
		t.Errorf("parsedInterval = %v", cfg.parsedInterval)
	}
	if len(cfg.Feeds) != 0 {
		t.Errorf("feeds = %d, want 0", len(cfg.Feeds))
	}
}
