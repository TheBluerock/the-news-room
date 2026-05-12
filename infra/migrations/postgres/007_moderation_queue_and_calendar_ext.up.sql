-- Extend editorial_calendar with optional editorial metadata
ALTER TABLE analytics_svc.editorial_calendar
    ADD COLUMN IF NOT EXISTS angle                TEXT,
    ADD COLUMN IF NOT EXISTS source_url           TEXT,
    ADD COLUMN IF NOT EXISTS journalist_profile_id UUID;

-- Moderation results queue — persists auto + human review decisions
CREATE SCHEMA IF NOT EXISTS moderation_svc;

CREATE TABLE IF NOT EXISTS moderation_svc.review_queue (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    article_id        UUID NOT NULL,
    market            TEXT NOT NULL CHECK (market IN ('italy', 'usa', 'china')),
    topic             TEXT,
    status            TEXT NOT NULL DEFAULT 'auto_approved'
                      CHECK (status IN ('auto_approved', 'auto_rejected', 'human_approved', 'human_rejected')),
    quality_score     FLOAT NOT NULL DEFAULT 0,
    cultural_ok       BOOLEAN,
    factual_ok        BOOLEAN,
    rejection_reasons TEXT[] NOT NULL DEFAULT '{}',
    reviewed_by       UUID,
    reviewed_at       TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS review_queue_market_status_idx
    ON moderation_svc.review_queue (market, status, created_at DESC);

CREATE INDEX IF NOT EXISTS review_queue_article_id_idx
    ON moderation_svc.review_queue (article_id);

-- moderation_rw: write results, update on human review
DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'moderation_rw') THEN
        CREATE ROLE moderation_rw;
    END IF;
END $$;

GRANT USAGE ON SCHEMA moderation_svc TO moderation_rw;
GRANT SELECT, INSERT, UPDATE ON ALL TABLES IN SCHEMA moderation_svc TO moderation_rw;
ALTER DEFAULT PRIVILEGES IN SCHEMA moderation_svc
    GRANT SELECT, INSERT, UPDATE ON TABLES TO moderation_rw;

-- analytics_rw: read-only access to moderation queue for cross-service analytics
GRANT USAGE ON SCHEMA moderation_svc TO analytics_rw;
GRANT SELECT ON ALL TABLES IN SCHEMA moderation_svc TO analytics_rw;
ALTER DEFAULT PRIVILEGES IN SCHEMA moderation_svc
    GRANT SELECT ON TABLES TO analytics_rw;
