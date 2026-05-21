-- Phase K1: cost tracking columns on analytics_svc.article_performance.
-- Captures LLM token usage + computed USD cost per article so we can:
--   1. Sum monthly spend per market/model for budget enforcement.
--   2. Compute quality_per_1k_tokens efficiency metric.
--   3. Power Grafana cost dashboard (Phase E).

ALTER TABLE analytics_svc.article_performance
    ADD COLUMN IF NOT EXISTS prompt_tokens          INTEGER,
    ADD COLUMN IF NOT EXISTS completion_tokens      INTEGER,
    ADD COLUMN IF NOT EXISTS model                  TEXT,
    ADD COLUMN IF NOT EXISTS cost_usd               NUMERIC(10, 6),
    ADD COLUMN IF NOT EXISTS quality_per_1k_tokens  DOUBLE PRECISION;

-- Index supports "current month spend per market" aggregation served by the
-- cost dashboard + kill-switch reconciliation cron.
CREATE INDEX IF NOT EXISTS article_performance_cost_idx
    ON analytics_svc.article_performance (market, created_at DESC)
    WHERE cost_usd IS NOT NULL;
