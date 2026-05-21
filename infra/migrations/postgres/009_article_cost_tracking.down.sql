-- Reverse Phase K1 cost tracking.
-- Order matters: drop the partial index before the columns it references.

DROP INDEX IF EXISTS analytics_svc.article_performance_cost_idx;

ALTER TABLE analytics_svc.article_performance
    DROP COLUMN IF EXISTS quality_per_1k_tokens,
    DROP COLUMN IF EXISTS cost_usd,
    DROP COLUMN IF EXISTS model,
    DROP COLUMN IF EXISTS completion_tokens,
    DROP COLUMN IF EXISTS prompt_tokens;
