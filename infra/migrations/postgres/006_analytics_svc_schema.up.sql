-- analytics_svc schema: article performance metrics, editorial calendar

CREATE SCHEMA IF NOT EXISTS analytics_svc;

-- Article quality and performance (populated by RecordQualityScore gRPC calls)
CREATE TABLE IF NOT EXISTS analytics_svc.article_performance (
    article_id    UUID PRIMARY KEY,
    market        TEXT NOT NULL CHECK (market IN ('italy', 'usa', 'china')),
    topic_id      TEXT,
    quality_score FLOAT NOT NULL DEFAULT 0,
    cultural_ok   BOOLEAN,
    factual_ok    BOOLEAN,
    published_at  TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS article_performance_market_idx
    ON analytics_svc.article_performance (market, created_at DESC);

-- Editorial calendar: editors schedule topics; publisher queries and emits topic.trending
CREATE TABLE IF NOT EXISTS analytics_svc.editorial_calendar (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    market        TEXT NOT NULL CHECK (market IN ('italy', 'usa', 'china')),
    topic_id      TEXT NOT NULL,
    topic_name    TEXT NOT NULL,
    scheduled_at  TIMESTAMPTZ NOT NULL,
    dispatched    BOOLEAN NOT NULL DEFAULT false,
    dispatched_at TIMESTAMPTZ,
    created_by    UUID,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Partial index: publisher queries WHERE dispatched = false — keep this index narrow
CREATE INDEX IF NOT EXISTS editorial_calendar_due_idx
    ON analytics_svc.editorial_calendar (scheduled_at)
    WHERE dispatched = false;

-- Best-effort copy from public performance table (schema mismatch — copy what maps cleanly)
INSERT INTO analytics_svc.article_performance (article_id, market, created_at)
SELECT p.article_id, a.market, p.updated_at
FROM performance p
JOIN articles a ON a.id = p.article_id
ON CONFLICT (article_id) DO NOTHING;

-- Per-service role with minimal privileges
DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'analytics_rw') THEN
        CREATE ROLE analytics_rw;
    END IF;
END $$;

GRANT USAGE ON SCHEMA analytics_svc TO analytics_rw;
GRANT SELECT, INSERT, UPDATE ON ALL TABLES IN SCHEMA analytics_svc TO analytics_rw;
GRANT USAGE ON SEQUENCE analytics_svc.editorial_calendar_id_seq TO analytics_rw;
ALTER DEFAULT PRIVILEGES IN SCHEMA analytics_svc GRANT SELECT, INSERT, UPDATE ON TABLES TO analytics_rw;
ALTER DEFAULT PRIVILEGES IN SCHEMA analytics_svc GRANT USAGE ON SEQUENCES TO analytics_rw;
