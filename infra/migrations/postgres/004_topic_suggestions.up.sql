CREATE TABLE IF NOT EXISTS learner_svc.topic_suggestions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    market       TEXT NOT NULL,
    topic_id     TEXT NOT NULL,
    topic_name   TEXT NOT NULL,
    source_count INTEGER NOT NULL DEFAULT 1,
    example_urls TEXT[] NOT NULL DEFAULT '{}',
    suggested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (market, topic_id)
);

CREATE INDEX topic_suggestions_market_idx ON learner_svc.topic_suggestions (market, suggested_at DESC);
