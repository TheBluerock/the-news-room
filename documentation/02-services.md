# 02 — Services

## Service Map

| Service | Language | Port (main) | Health port | Domain |
|---------|----------|-------------|-------------|--------|
| `auth` | Go | 8080 (gRPC) | 8090 | JWT issuance, validation, RBAC |
| `learner` | Go | 8080 (gRPC) | 8090 | Knowledge graph, journalist profiles, HNSW embeddings |
| `agent` | Python | — (consumer only) | 8090 | Article generation LangGraph pipeline |
| `moderation` | Python | — (consumer only) | 8090 | Cultural + factual checks, gating |
| `correction` | Go | — (consumer only) | 8090 | Editor correction fast-path to Redis |
| `analytics` | Go | 8080 (gRPC) | 8090 | Trend detection, quality scoring, article telemetry |

All health endpoints share the same contract regardless of service:
- `GET /health` — always 200 if process is alive; never fails on downstream unavailability
- `GET /ready` — 200 only when all deps (DB, Redis, Vault, gRPC upstreams) are verified
- `GET /metrics` — Prometheus exposition format

**Health check and per-market degradation:** `/ready` is all-or-nothing. For the agent service, a Redis outage fails `/ready` for all three markets simultaneously even if only one market's pipeline is affected. This is intentional: at v4.1 scale, partial degradation adds operational complexity that isn't worth it. If per-market independent scaling becomes a requirement (see `09-open-issues.md` REF-05), the per-market agent split would naturally enable per-market readiness checks.

---

## auth

**Owns:** JWT RS256 key pair management, token issuance and validation, Casbin RBAC policy enforcement, JWT blocklist.

**Does not own:** Any article, market, or journalist data.

**Calls:** Nothing at startup. Other services call auth to validate tokens.

**Redis roles:** JWT blocklist (`jwt:blocked:<jti>`).

**Key constraints:**
- RS256 key rotation uses a 15-minute overlap window — both old and new keys are valid simultaneously during rotation
- Revoked tokens are added to Redis blocklist immediately; blocklist TTL matches token expiry
- RBAC policy is loaded from PostgreSQL `casbin_rule` table at startup and cached in memory

---

## learner

**Owns:** Knowledge graph (articles, topics, sources, journalist profiles), semantic embeddings, HNSW vector index management.

**Does not own:** Article generation, moderation decisions, corrections in-flight.

**gRPC methods exposed:**
- `QueryKnowledgeGraph(market, query_text, top_k)` → context fragments for agent
- `GetJournalistProfile(journalist_id)` → writing style, past articles, tone preferences
- `ScoreFactualAccuracy(content, market)` → factual confidence score (used by moderation)
- `GetTopicSuggestions(market, limit)` → editorial topic suggestions for the frontend

**HTTP endpoint (port 8088, for frontend):**
- `GET /suggestions?market=italy&limit=10` → same data as `GetTopicSuggestions`, JSON array

**Consumes (via learner-ingest):** `editor.correction` (slow path — updates PostgreSQL, regenerates embeddings, then DELs Redis correction key), `moderation.rejected` (trains negative examples), `article.approved` (indexes article embedding)

**learner-ingest also runs:** scheduled RSS scraper per market (6h interval) → after each scrape, calls OpenAI once to extract top topic suggestions → upserts into `learner_svc.topic_suggestions`

**Redis roles:** HNSW vector index (`vectors:<market>:*`), short-term memory (`memory:<market>`), correction key DEL after slow-path apply.

> After the planned refactor, Learner absorbs the correction service entirely. The correction service's only job (Redis write) becomes a thin function inside Learner's `editor.correction` consumer.

---

## agent

**Owns:** LangGraph article generation pipeline, per-market semaphore (max 2 concurrent), LLM orchestration, circuit breaker state reads.

**Does not own:** Circuit breaker state writes (that's the pipeline itself, via `circuit.py`), correction data (reads from Redis), trending signals (reads from event payload).

**Consumes:** `topic.trending` (triggers one pipeline run per event)

**Produces:** `article.generated`

**gRPC calls made:**
- `LearnerService.QueryKnowledgeGraph` (context step)
- `AnalyticsService.RecordQualityScore` (after generation, fire-and-forget)

**Redis roles:** HNSW semantic search (read), short-term memory (read+write, 7-day TTL), corrections (read only), circuit breaker state (read+write), rate limiter token bucket (read+write).

**See:** `05-agent-pipeline.md` for full LangGraph node documentation.

---

## moderation

**Owns:** Cultural sensitivity check, factual accuracy check, quality scoring heuristic.

**Does not own:** The article content (read-only), publication decision (that's human + Sanity).

**Consumes:** `article.generated`

**Produces:** `article.approved`, `moderation.rejected`

**LLM calls:** Two OpenAI GPT-4o calls per article (cultural check + factual check), each returns structured JSON. No Anthropic fallback in moderation — circuit breaker lives in the agent layer only.

**Quality scoring:** Heuristic only (word count, sentence length distribution, keyword density). No LLM call.

**Threshold:** `cultural_ok AND factual_ok AND quality_score >= 0.5` → approved. Any failure → rejected with specific reason.

---

## correction

**Owns:** Redis correction fast-path writes. Nothing else.

**Does not own:** The correction data model (that's learner's schema), slow-path PostgreSQL updates (that's learner).

**Consumes:** `editor.correction`, `moderation.rejected`

**Produces:** Nothing (writes to Redis only)

**Redis roles:** `corrections:<market>:<correction_id>` (48h TTL), `correction_ttl_remaining_seconds` Prometheus gauge.

**Retry strategy:** 5s → 30s → 2m → DLQ (`editor.correction.dlq`, `moderation.rejected.dlq`)

> This service is intentionally thin and is a candidate for absorption into Learner in the next refactor phase (see `09-open-issues.md` REF-01).

---

## analytics

**Owns:** Editorial calendar (what topics to cover, when), quality score storage, article performance reporting.

**Does not own:** Article content, moderation decisions. Does not decide *how* topics are generated — only *when* to trigger them.

**gRPC methods exposed:**
- `RecordQualityScore(article_id, market, score, checks)` → upserts performance table
- `RecordPublishedArticle(market, topic_id)` → called by published consumer

**Consumes:** `article.published` (tracker — updates performance DB)

**Produces:** `topic.trending` (calendar publisher — at scheduled time per calendar entry)

**Editorial calendar model:** Editors schedule coverage via the frontend admin UI. Each calendar entry specifies `market`, `topic_id`, `topic_name`, and `scheduled_at`. The Analytics publisher ticks every minute, queries for entries due in the current window, emits `topic.trending`, and marks them as dispatched. This replaces algorithmic trending scoring entirely.

**Feedback loop (read-only):** The Grafana analytics dashboard shows editors which topics generated high-quality articles, which were rejected, and which performed well — informing their next scheduling decisions. This data does not automatically trigger new coverage.

**Redis roles:** Performance cache for recently published articles (short-term dedup check).
