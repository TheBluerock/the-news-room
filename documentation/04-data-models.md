# 04 — Data Models

## PostgreSQL

All services share one PostgreSQL 16 instance but use separate schemas with separate roles. This prevents cross-service table access at the database layer.

> **Current state (v4.1):** A single `newsroom` schema exists. The per-schema split is approved but not yet implemented. See `09-open-issues.md` REF-02.

### auth_svc schema

```sql
-- JWT RS256 key pairs
CREATE TABLE auth_svc.jwt_keys (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    public_key  TEXT NOT NULL,
    private_key TEXT NOT NULL,  -- encrypted at rest
    active      BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NOT NULL
);

-- Casbin RBAC rules
CREATE TABLE auth_svc.casbin_rule (
    id      BIGSERIAL PRIMARY KEY,
    ptype   TEXT NOT NULL,  -- p, g
    v0      TEXT, v1 TEXT, v2 TEXT, v3 TEXT, v4 TEXT, v5 TEXT
);

-- Users
CREATE TABLE auth_svc.users (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email      TEXT UNIQUE NOT NULL,
    role       TEXT NOT NULL,  -- admin, editor, viewer
    market     TEXT,           -- null = all markets
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ     -- soft delete for GDPR
);
```

### learner_svc schema

```sql
-- Journalist writing profiles
CREATE TABLE learner_svc.journalist_profiles (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name         TEXT NOT NULL,
    market       TEXT NOT NULL,
    tone         TEXT,
    style_notes  TEXT,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Knowledge graph: topics and sources
CREATE TABLE learner_svc.topics (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    market     TEXT NOT NULL,
    name       TEXT NOT NULL,
    category   TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE learner_svc.sources (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    market     TEXT NOT NULL,
    url        TEXT,
    title      TEXT,
    content    TEXT,
    fetched_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Article embeddings (pgvector, dimension 1536 for OpenAI ada-002)
CREATE TABLE learner_svc.article_embeddings (
    article_id  UUID PRIMARY KEY,
    market      TEXT NOT NULL,
    embedding   VECTOR(1536),
    stale       BOOLEAN NOT NULL DEFAULT false,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### analytics_svc schema

```sql
-- Article quality and performance
CREATE TABLE analytics_svc.article_performance (
    article_id      UUID PRIMARY KEY,
    market          TEXT NOT NULL,
    topic_id        TEXT,
    quality_score   FLOAT NOT NULL,
    cultural_ok     BOOLEAN,
    factual_ok      BOOLEAN,
    published_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Editorial calendar: editors schedule what to cover and when
CREATE TABLE analytics_svc.editorial_calendar (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    market       TEXT NOT NULL,
    topic_id     TEXT NOT NULL,
    topic_name   TEXT NOT NULL,
    scheduled_at TIMESTAMPTZ NOT NULL,
    dispatched   BOOLEAN NOT NULL DEFAULT false,
    dispatched_at TIMESTAMPTZ,
    created_by   UUID,                    -- editor user_id
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX editorial_calendar_due_idx
    ON analytics_svc.editorial_calendar (scheduled_at)
    WHERE dispatched = false;
```

### Audit log (append-only, shared)

```sql
CREATE TABLE audit_log (
    id          BIGSERIAL PRIMARY KEY,
    event_type  TEXT NOT NULL,
    actor_id    UUID,
    target_id   UUID,
    market      TEXT,
    payload     JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- No UPDATE or DELETE privilege granted to any service role.
-- Retained 1 year, then archived to S3.
```

**Payload schema evolution:** The `payload` JSONB column is flexible by design. When a new `event_type` is introduced, include an optional `_version` field (integer, default 1). If a future change to an existing event type's structure is required, increment `_version` and update all consumers to handle both versions. Changing the shape of an existing event type without incrementing `_version` breaks historical queries.

---

## Redis Key Taxonomy

All Redis keys use `:` as namespace separator.

### HNSW Vector Index (RediSearch)

| Key pattern | Description |
|-------------|-------------|
| `vectors:<market>:<article_id>` | Article embedding for HNSW search |
| `vector:stale:<article_id>` | Marker — skip in semantic search until learner re-indexes |

Index names: `idx:articles:italy`, `idx:articles:usa`, `idx:articles:china`

Embeddings are batched 100 per `HSET` call. Dimension: 1536 (OpenAI ada-002).

**Consistency between Redis and PostgreSQL:** Redis is the hot cache; PostgreSQL `article_embeddings` is the source of truth. If a Redis write fails after a PostgreSQL write, the article's embedding is missing from HNSW search until Learner re-indexes it. The `vector:stale:<article_id>` key is set by Learner before re-indexing to suppress the article from search during the window. If Redis is wiped entirely (e.g. disaster recovery), embeddings must be re-indexed from PostgreSQL — Learner provides a `make reindex-vectors ENV=<env>` recovery target. There is no automatic reconciliation cron; stale counts are visible via the `vectors_stale_total{market}` Prometheus gauge.

### Short-term Memory (Agent)

| Key | Type | TTL | Description |
|-----|------|-----|-------------|
| `memory:<market>` | Hash | 7 days | Recent topics, sources, angles used by agent |

Fields: `last_topic`, `last_source`, `last_angle`, `last_updated`

### Correction Fast-Path

| Key | Type | TTL | Description |
|-----|------|-----|-------------|
| `corrections:<market>:<correction_id>` | Hash | 48 hours | Editor correction or rejection signal |

Fields: `article_id`, `field`, `old_value`, `new_value`, `reason`, `created_at`

Alert fires when `correction_ttl_remaining_seconds < 3600` — signals Learner slow path is delayed.

Learner DELs the key after applying the correction to PostgreSQL.

### LLM Circuit Breaker

| Key | Type | TTL | Description |
|-----|------|-----|-------------|
| `circuit:<market>:<provider>:state` | String | — | `0`=CLOSED, `1`=OPEN, `2`=HALF_OPEN |
| `circuit:<market>:<provider>:failures` | String | 60s | Failure count in current window |
| `circuit:<market>:<provider>:opened_at` | String | — | Unix timestamp when circuit tripped |

`<provider>` is `openai` or `anthropic`. Market is one of `italy`, `usa`, `china`.

### Rate Limiter (Token Bucket)

| Key | Type | Description |
|-----|------|-------------|
| `llm:rate:<market>` | String (float) | Current token count |
| `llm:rate:<market>:last_refill` | String (float) | Unix timestamp of last refill |

Capacity: 10 tokens. Refill rate: 1 token/minute. Uses Redis WATCH/MULTI/EXEC for optimistic locking.

### JWT Blocklist

| Key | Type | TTL | Description |
|-----|------|-----|-------------|
| `jwt:blocked:<jti>` | String | Token expiry | Revoked token marker |

### Analytics Performance Cache

| Key | Type | TTL | Description |
|-----|------|-----|-------------|
| `published:<market>:<article_id>` | String | 7 days | Dedup marker — prevents learner-ingest from re-indexing an article already processed |

---

## Vault Secret Paths

All services read secrets from `/vault/secrets/<service>` on the shared tmpfs volume written by the Vault Agent sidecar (K8s) or entrypoint script (Swarm/Compose).

| Service | Vault path | Keys |
|---------|-----------|------|
| auth | `secret/data/auth` | `postgres_dsn`, `redis_url`, `jwt_private_key`, `jwt_public_key` |
| learner | `secret/data/learner` | `postgres_dsn`, `redis_url`, `openai_api_key` |
| agent | `secret/data/agent` | `redis_url`, `openai_api_key`, `anthropic_api_key`, `redpanda_brokers`, `learner_grpc_addr`, `analytics_grpc_addr` |
| moderation | `secret/data/moderation` | `openai_api_key`, `redpanda_brokers` |
| correction | `secret/data/correction` | `redis_url`, `redpanda_brokers` |
| analytics | `secret/data/analytics` | `postgres_dsn`, `redis_url`, `redpanda_brokers` |

**Local dev:** `make vault-seed` populates these paths in Vault dev mode with dummy credentials.

**Auto-rotation:** PostgreSQL and Redis passwords rotate every 30 days. JWT RS256 keys rotate with a 15-minute overlap window. Services re-read `/vault/secrets/` on a configurable interval (default: 5 minutes); no restart required.

**Connection pool behavior during rotation:** When a service detects a new `postgres_dsn` in `/vault/secrets/`, it opens a new connection pool with the new credentials and drains the old pool gracefully — in-flight requests complete on the old connection, new requests go to the new pool. The old pool is closed after all connections return or after a 30-second drain timeout. This is an atomic swap; there is no window where new and old credentials are mixed within a single transaction.

> Never pass credentials via environment variables. Never log the contents of `/vault/secrets/`. Never commit values from these paths to git.
