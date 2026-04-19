-- learner_svc schema: knowledge graph, embeddings, corrections, rejections

CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE SCHEMA IF NOT EXISTS learner_svc;

-- Journalist writing profiles
CREATE TABLE IF NOT EXISTS learner_svc.journalist_profiles (
    journalist_id   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    market          TEXT NOT NULL,
    name            TEXT NOT NULL,
    specialization  TEXT,
    style_profile   JSONB,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Scraped external sources with embeddings
CREATE TABLE IF NOT EXISTS learner_svc.sources (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    market      TEXT NOT NULL,
    url         TEXT UNIQUE NOT NULL,
    title       TEXT,
    content     TEXT,
    embedding   VECTOR(1536),
    fetched_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS sources_market_idx ON learner_svc.sources (market);
CREATE INDEX IF NOT EXISTS sources_fts_idx ON learner_svc.sources
    USING GIN (to_tsvector('english', coalesce(title, '') || ' ' || coalesce(content, '')));
CREATE INDEX IF NOT EXISTS sources_embedding_idx ON learner_svc.sources
    USING hnsw (embedding vector_cosine_ops);

-- Article embeddings (indexed from article.approved events)
CREATE TABLE IF NOT EXISTS learner_svc.article_embeddings (
    article_id  UUID PRIMARY KEY,
    market      TEXT NOT NULL,
    embedding   VECTOR(1536),
    stale       BOOLEAN NOT NULL DEFAULT false,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS article_embeddings_market_idx ON learner_svc.article_embeddings (market);
CREATE INDEX IF NOT EXISTS article_embeddings_vec_idx ON learner_svc.article_embeddings
    USING hnsw (embedding vector_cosine_ops) WHERE stale = false;

-- Editor corrections slow-path log
CREATE TABLE IF NOT EXISTS learner_svc.corrections (
    correction_id   TEXT PRIMARY KEY,
    market          TEXT NOT NULL,
    correction_type TEXT,
    reason          TEXT,
    old_value       TEXT,
    new_value       TEXT,
    applied_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Moderation rejection log (negative training examples)
CREATE TABLE IF NOT EXISTS learner_svc.rejections (
    article_id  UUID PRIMARY KEY,
    market      TEXT NOT NULL,
    reason      TEXT,
    logged_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Roles (created only if not existing)
DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'learner_rw') THEN
        CREATE ROLE learner_rw;
    END IF;
END $$;

GRANT USAGE ON SCHEMA learner_svc TO learner_rw;
GRANT SELECT, INSERT, UPDATE ON ALL TABLES IN SCHEMA learner_svc TO learner_rw;
ALTER DEFAULT PRIVILEGES IN SCHEMA learner_svc GRANT SELECT, INSERT, UPDATE ON TABLES TO learner_rw;
