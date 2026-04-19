package scraper

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mmcdole/gofeed"
	"go.yaml.in/yaml/v2"

	"github.com/newsroom/learner/internal/db"
	"github.com/newsroom/learner/internal/embeddings"
	"github.com/newsroom/learner/internal/suggestions"
)

// MarketConfig is loaded from config/sources_<market>.yaml.
type MarketConfig struct {
	Market          string        `yaml:"market"`
	ScrapeInterval  string        `yaml:"scrape_interval"`
	Feeds           []FeedConfig  `yaml:"feeds"`
	parsedInterval  time.Duration
}

// FeedConfig is one RSS/Atom feed entry.
type FeedConfig struct {
	URL      string `yaml:"url"`
	Category string `yaml:"category"`
}

// LoadConfig reads a market config YAML file.
func LoadConfig(path string) (*MarketConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg MarketConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	d, err := time.ParseDuration(cfg.ScrapeInterval)
	if err != nil {
		d = 6 * time.Hour
	}
	cfg.parsedInterval = d
	return &cfg, nil
}

// RunScraper runs a periodic scraper for one market. Blocks until ctx is cancelled.
func RunScraper(ctx context.Context, cfg *MarketConfig, pool *pgxpool.Pool, openAIKey string, logger *slog.Logger) {
	logger.Info("scraper starting", "market", cfg.Market, "interval", cfg.parsedInterval, "feeds", len(cfg.Feeds))

	// Run immediately on startup, then on ticker
	runOnce(ctx, cfg, pool, openAIKey, logger)

	ticker := time.NewTicker(cfg.parsedInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runOnce(ctx, cfg, pool, openAIKey, logger)
		}
	}
}

func runOnce(ctx context.Context, cfg *MarketConfig, pool *pgxpool.Pool, openAIKey string, logger *slog.Logger) {
	parser := gofeed.NewParser()
	for _, feed := range cfg.Feeds {
		if ctx.Err() != nil {
			return
		}
		if err := scrapeFeed(ctx, parser, cfg.Market, feed, pool, openAIKey, logger); err != nil {
			logger.Error("feed scrape failed", "url", feed.URL, "err", err)
		}
	}

	// After all feeds are scraped, extract topic suggestions from recent titles.
	// One LLM call per market per scrape cycle.
	refreshSuggestions(ctx, cfg.Market, pool, openAIKey, logger)
}

func refreshSuggestions(ctx context.Context, market string, pool *pgxpool.Pool, openAIKey string, logger *slog.Logger) {
	titles, urls, err := db.RecentSourceTitles(ctx, pool, market, 7, 40)
	if err != nil || len(titles) == 0 {
		return
	}

	extracted, err := suggestions.ExtractFromTitles(ctx, openAIKey, market, titles, urls, 10)
	if err != nil {
		logger.Warn("topic suggestion extraction failed", "market", market, "err", err)
		return
	}

	for _, s := range extracted {
		if err := db.UpsertTopicSuggestion(ctx, pool, market, db.TopicSuggestionRow{
			TopicID:     s.TopicID,
			TopicName:   s.TopicName,
			SourceCount: s.SourceCount,
			ExampleURLs: s.ExampleURLs,
		}); err != nil {
			logger.Warn("upsert suggestion failed", "topic_id", s.TopicID, "err", err)
		}
	}
	logger.Info("topic suggestions refreshed", "market", market, "count", len(extracted))
}

func scrapeFeed(ctx context.Context, parser *gofeed.Parser, market string, feed FeedConfig, pool *pgxpool.Pool, openAIKey string, logger *slog.Logger) error {
	parsed, err := parser.ParseURLWithContext(feed.URL, ctx)
	if err != nil {
		return err
	}

	new := 0
	for _, item := range parsed.Items {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		url := item.Link
		if url == "" {
			continue
		}

		// Skip if recently scraped (within 80% of scrape interval to avoid boundary races)
		exists, _ := db.SourceExists(ctx, pool, url, 4*time.Hour)
		if exists {
			continue
		}

		content := item.Content
		if content == "" {
			content = item.Description
		}
		title := item.Title

		text := title + "\n\n" + content
		if len(text) > 8000 {
			text = text[:8000] // keep within embedding token limit
		}

		embedding, err := embeddings.Generate(ctx, openAIKey, text)
		if err != nil {
			logger.Warn("embedding failed, storing without vector", "url", url, "err", err)
			embedding = nil
		}

		if embedding != nil {
			if err := db.UpsertSource(ctx, pool, market, url, title, content, embedding); err != nil {
				logger.Error("upsert source failed", "url", url, "err", err)
				continue
			}
		}
		new++
	}

	if new > 0 {
		logger.Info("scraped feed", "market", market, "url", feed.URL, "new_items", new)
	}
	return nil
}
