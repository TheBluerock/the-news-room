CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- ── Users & RBAC ─────────────────────────────────────────────────────────────

CREATE TABLE users (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email      TEXT UNIQUE NOT NULL,
    market     TEXT CHECK (market IN ('italy', 'usa', 'china')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Casbin rule table (native PostgreSQL adapter format)
CREATE TABLE casbin_rule (
    id    BIGSERIAL PRIMARY KEY,
    ptype TEXT NOT NULL,
    v0    TEXT, v1 TEXT, v2 TEXT,
    v3    TEXT, v4 TEXT, v5 TEXT
);

-- Seed default roles: Admin / Editor / Journalist / Viewer per market
INSERT INTO casbin_rule (ptype, v0, v1, v2) VALUES
    ('p', 'admin',       '*',        '*'),
    ('p', 'editor',      'articles', 'read'),
    ('p', 'editor',      'articles', 'approve'),
    ('p', 'editor',      'articles', 'correct'),
    ('p', 'journalist',  'articles', 'read'),
    ('p', 'viewer',      'articles', 'read');

-- ── Knowledge Graph ───────────────────────────────────────────────────────────

CREATE TABLE journalists (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name           TEXT NOT NULL,
    market         TEXT NOT NULL CHECK (market IN ('italy', 'usa', 'china')),
    specialization TEXT,
    style_profile  JSONB NOT NULL DEFAULT '{}',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE articles (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    journalist_id UUID REFERENCES journalists(id),
    market       TEXT NOT NULL CHECK (market IN ('italy', 'usa', 'china')),
    language     TEXT NOT NULL CHECK (language IN ('it', 'en', 'zh')),
    content      TEXT,
    sanity_id    TEXT UNIQUE,
    published_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE topics (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    market      TEXT NOT NULL CHECK (market IN ('italy', 'usa', 'china')),
    trend_score FLOAT NOT NULL DEFAULT 0,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (name, market)
);

CREATE TABLE performance (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    article_id     UUID NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    views          BIGINT NOT NULL DEFAULT 0,
    engagement     FLOAT NOT NULL DEFAULT 0,
    editor_approved BOOLEAN,
    corrections    INTEGER NOT NULL DEFAULT 0,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE edges (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    from_id       UUID NOT NULL,
    to_id         UUID NOT NULL,
    relation_type TEXT NOT NULL,
    weight        FLOAT NOT NULL DEFAULT 1.0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_edges_from ON edges(from_id);
CREATE INDEX idx_edges_to   ON edges(to_id);
CREATE INDEX idx_articles_market ON articles(market);
CREATE INDEX idx_topics_market   ON topics(market, trend_score DESC);

-- ── Immutable Audit Log ───────────────────────────────────────────────────────

CREATE TABLE audit_log (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    timestamp   TIMESTAMPTZ NOT NULL DEFAULT now(),
    user_id     UUID NOT NULL REFERENCES users(id),
    action      TEXT NOT NULL, -- approved | rejected | corrected | role_changed | correction_reversed
    resource_id UUID NOT NULL,
    market      TEXT,
    old_value   JSONB,
    new_value   JSONB
);

-- Service role cannot modify audit records — enforce in Vault/app bootstrap:
-- REVOKE UPDATE, DELETE ON audit_log FROM newsroom_service;

CREATE INDEX idx_audit_log_resource ON audit_log(resource_id);
CREATE INDEX idx_audit_log_user     ON audit_log(user_id);
CREATE INDEX idx_audit_log_time     ON audit_log(timestamp DESC);
