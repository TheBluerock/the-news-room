# 09 — Open Issues

All architectural gaps, pending refactors, and known issues. Each item has an ID, priority, status, and the approved solution or decision.

When an item is resolved, update **Status** and add a **Resolution** note.

Priority levels use Italian terms consistent with the project's cultural scope:
- **CRITICO** — blocks correctness or reliability; fix before any Phase 4 work
- **ALTO** — significant tech debt or operational risk; fix before launch
- **MEDIO** — quality-of-life or performance; strongly recommended
- **BASSO** — nice-to-have; defer without consequence

---

## REF-01 — ALTO: Correction service is too thin; should be absorbed into Learner

**Sections:** `02-services.md`, `04-data-models.md`
**Status:** Approved solution, pending implementation

**Problem:** The correction service does exactly one thing: write a hash to Redis with a 48h TTL. This is 50 lines of code deployed as an independent service. It has its own Dockerfile, its own Vault path, its own health port, and its own consumer group. The operational overhead is not justified.

**Approved solution:**
- Move `services/correction/internal/processor/redis.go` logic into Learner as a new `fastpath` package
- Learner consumes `editor.correction` on two separate consumer groups: one for fast-path Redis write (immediate), one for slow-path PostgreSQL update (with existing 1–6h schedule)
- The `moderation.rejected` consumer in correction service also moves to Learner
- Delete `services/correction/` entirely
- Update `docker-compose.dev.yml`, Vault seed, and RedPanda consumer group setup accordingly

**Benefit:** One fewer service to operate, clearer ownership (Learner owns the full correction lifecycle), simpler network topology.

---

## REF-02 — ALTO: Single PostgreSQL schema — no per-service isolation

**Sections:** `04-data-models.md`
**Status:** Resolved — migrations 003 (learner_svc), 005 (auth_svc), 006 (analytics_svc) created schemas, roles, and data copies. Vault-seed DSNs updated with per-schema search_path. Service query layer not yet updated to use schema-qualified names (unblocked by search_path).

**Problem:** All services share the `newsroom` (public) schema with a single Postgres role. A bug in analytics can accidentally query learner tables. Schema drift is hard to detect. There's no DB-layer enforcement of service boundaries.

**Approved solution:**
- Create separate schemas: `auth_svc`, `learner_svc`, `analytics_svc`
- Create separate Postgres roles with GRANT permissions scoped to their schema only
- Update Vault dynamic secret paths to issue per-service roles
- Migrate existing tables to new schemas via golang-migrate steps (backward-compatible: create new schema, copy data, update role, deprecate old tables)
- Update all service connection strings and query paths

**Implementation order:** Learner first (largest table set), then analytics, then auth. Correction merges into Learner (REF-01) before this migration.

---

## REF-03 — ALTO: Agent→Analytics gRPC call is redundant

**Sections:** `05-agent-pipeline.md`, `02-services.md`
**Status:** Resolved — `get_trending_signals` removed from client, `fetch_analytics` node/edges/function removed from pipeline, `analytics` field removed from `ArticleState`. `analytics_channel` plumbing retained in `main.py`/`consumer.py` for future `record_quality` wiring.

**Problem:** The original pipeline design included a `fetch_analytics` node that called `AnalyticsService.GetTrendingSignals` via gRPC. This data was already present in the `topic.trending` event that triggered the pipeline. The node was identified as a duplicate roundtrip and removed from the LangGraph graph.

However, `services/agent/pipeline/analytics_client.py` still contains the `get_trending_signals()` function and the gRPC channel is still wired in `services/agent/main.py`.

**Approved solution:**
- Remove `get_trending_signals()` from `analytics_client.py`
- Keep `record_quality_score()` in `analytics_client.py` — that call is still needed post-generation
- If `analytics_client.py` becomes a single function, inline it into `pipeline/generate.py` or the `publish` node
- Remove unused `analytics_channel` from `ArticleState` if no longer needed

---

## REF-04 — ALTO: No Docker Swarm stack files — infrastructure target not implemented

**Sections:** `06-infrastructure.md`
**Status:** Approved, pending implementation

**Problem:** CLAUDE.md and architecture discussions specify Docker Swarm as the production target, but `infra/` contains only Helm charts for Kubernetes. The Swarm stack files don't exist.

**Approved solution:**
- Create `infra/swarm/stack.dev.yml` — single-node local Swarm equivalent of `docker-compose.dev.yml`
- Create `infra/swarm/stack.prod.yml` — multi-node production Swarm with replicas, resource limits, and secrets via Docker Swarm secrets or Vault entrypoint
- Create `infra/docker/entrypoint.sh` — Vault AppRole authentication and secret fetch before exec
- Update `Makefile` with `swarm-deploy`, `swarm-status`, `swarm-rollback` targets
- Keep Helm charts for reference but mark as deprecated in comments

---

## REF-05 — MEDIO: Agent should be split into per-market instances

**Sections:** `02-services.md`, `05-agent-pipeline.md`
**Status:** Discussed, not yet formally approved

**Problem:** A single agent service handles all three markets via in-process semaphores. This means a bug in the Italy pipeline can affect USA and China. Configuration, prompts, and rate limits are mixed in one process. Scaling one market independently is not possible.

**Proposed solution:**
- Split into `agent-italy`, `agent-usa`, `agent-china` — three separate deployable units
- Each subscribes to `topic.trending` with a market filter (or separate per-market topics)
- Each has its own personality config file (`config/italy.yaml`, `config/usa.yaml`, `config/china.yaml`)
- Each has its own rate limiter bucket, circuit breakers, and semaphore
- Shared code stays in `services/agent/pipeline/` — only `main.py` and config differ
- Docker Swarm allows independent scaling: `docker service scale newsroom_agent-italy=2`

**Tradeoff:** Three services to deploy instead of one. CI pipeline must build all three on agent code change.

---

## PHASE4-00 — CRITICO: Analytics publisher still uses algorithmic trending scoring

**Sections:** `02-services.md`, `04-data-models.md`
**Status:** Approved, must precede PHASE4-01 and PHASE4-02

**Problem:** The current Analytics publisher (`services/analytics/internal/trends/publisher.go`) fetches topics from a Redis sorted set scored by published article count. This is a closed feedback loop — we cover what we already covered. There is no editorial intent, no external signal, no human decision.

**Approved solution:**
- Add `analytics_svc.editorial_calendar` table (migration `004_editorial_calendar.up.sql`)
- Rewrite publisher to query `WHERE scheduled_at <= now() AND dispatched = false`, emit one `topic.trending` per row, update `dispatched = true, dispatched_at = now()`
- Remove Redis `trending:<market>` sorted set entirely
- Remove `UpdateTrendingCache` and `GetTrendingSignals` gRPC methods from Analytics (they served the old model; agent no longer calls them since REF-03)
- The frontend (PHASE4-02) exposes `GET/POST/DELETE /api/calendar/:market` for editors to schedule coverage

**Why editorial over algorithmic:** "Trending" in journalism means editorially significant, not statistically frequent. An algorithm that boosts topics based on past coverage creates a filter bubble. Editors should decide the calendar; the AI executes it.

---

## PHASE4-01 — RESOLVED: Sanity CMS integration implemented

**Sections:** `03-event-flow.md`
**Status:** Phase 4 pending
**Prerequisites:** REF-02 (per-service DB schemas) should be completed before this — building the Sanity connector on a shared schema creates access patterns that are harder to migrate later.

**Problem:** The event chain includes `article.approved → Sanity → article.published` but Sanity integration is not built. `article.approved` events are currently consumed by nothing — they accumulate in RedPanda without effect.

**Approved solution (Phase 4):**
- Build a Sanity connector service (Go) that consumes `article.approved` and calls Sanity API to create a draft document
- Use `article.id` as the Sanity document `_id` idempotency key — safe to replay
- Sanity webhook (on human editor publish action) calls a connector endpoint that produces `article.published`
- `article.published` triggers analytics tracking and closes the feedback loop
- Sanity document schema: `article` type with fields for `market`, `language`, `content`, `quality_score`, `approved_at`

---

## PHASE4-02 — ALTO: Public site not implemented

**Sections:** `07-auth.md`
**Status:** Phase 4 pending

**Problem:** No public site exists to render published articles to readers. Editors also have no UI for corrections or moderation, but that UI lives in a separate Admin app outside this repo.

**Approved solution (Phase 4) — public site only:**
- Astro app at `frontend/` with Svelte components for any interactivity
- Static generation (SSG) — articles built at deploy time, no SSR runtime
- Hosted on Vercel (static deployment, no serverless functions in this app)
- Three locales: `it`, `en`, `zh` via Astro built-in i18n routing
- Pages: article list (per market), article detail
- No authentication, no correction form, no moderation queue — those belong to the Admin app
- Sanity webhook → CI trigger that rebuilds the static site when `article.published` fires

**Out of scope for this repo:** Admin UI (editorial dashboard, moderation queue, correction form, analytics dashboard). The admin app consumes the same gRPC/HTTP gateway but is developed and deployed independently.

---

## LEARNER-01 — ALTO: Learner gRPC methods are stubs

**Sections:** `02-services.md`, `05-agent-pipeline.md`
**Status:** Phase 3b pending

**Problem:** `LearnerService.QueryKnowledgeGraph`, `GetJournalistProfile`, and `ScoreFactualAccuracy` are declared in proto and called by agent and moderation, but the Learner service returns empty/stub responses. The agent's `fetch_context` node always gets an empty context list.

**Approved solution:**
- Implement `QueryKnowledgeGraph`: full-text search + pgvector similarity on `learner_svc.article_embeddings` and `learner_svc.sources`
- Implement `GetJournalistProfile`: query `learner_svc.journalist_profiles` by ID
- Implement `ScoreFactualAccuracy`: call OpenAI with a factual verification prompt, return confidence score
- Implement `IndexArticle`: generate embedding via OpenAI ada-002, upsert into `learner_svc.article_embeddings` and Redis HNSW index
- Add `article.published` consumer in Learner to trigger `IndexArticle`

---

## OPS-01 — MEDIO: No per-service GitHub Actions CI pipelines

**Sections:** `06-infrastructure.md`
**Status:** Phase 5 pending

**Problem:** `.github/workflows/` is empty. There are no CI pipelines for any service.

**Approved solution (Phase 5):**
- Per-service workflow files with path filtering: `on: push: paths: ['services/agent/**']`
- Pipeline stages: `lint → test → build-image → push-to-registry`
- On merge to main: additionally `run-migrations → deploy-staging → canary-to-prod`
- Canary via Argo Rollouts: 5% → 50% → 100% over 10 minutes, auto-rollback on error rate > 1% or p99 latency +50%
- Vault service token stored in GitHub Secrets; all other credentials fetched from Vault at deploy time

---

## LEARN-01 — MEDIO: Quality scores never fed back into agent context

**Sections:** `05-agent-pipeline.md`, `04-data-models.md`
**Status:** Identified gap, no approved solution yet

**Problem:** `analytics_svc.article_performance.quality_score` is populated by `RecordQualityScore` gRPC calls after moderation, but nothing reads it back into the agent pipeline. The agent has no signal about which past articles scored well or poorly — it cannot learn to favour approaches that produce high-quality output.

Three feedback signals currently reach the agent at generation time:
- Editor corrections (fast-path Redis) ✅
- Knowledge graph context from past articles (HNSW embeddings) ✅
- Scraped source context ✅

One signal is captured but unused:
- Quality scores from moderation (`article_performance.quality_score`) ❌ — stored, never read

**Impact:** Agent may repeat low-scoring angles indefinitely. High-quality article patterns are not reinforced. Context improves via volume (more embeddings) but not via quality signal.

**Proposed solutions (not yet approved):**

Option A — Quality-weighted HNSW retrieval: when Learner indexes an article embedding, store `quality_score` alongside it. `SearchSimilar` in `db/queries.go` adds `WHERE quality_score >= 0.7` or uses score as a retrieval weight multiplier. High-quality articles surface more in `semantic_search` context.

Option B — Quality summary in system prompt: agent `_build_prompt` fetches average quality score per market from `article_performance` and appends a line like `"Recent quality average for italy: 0.82. Avoid: [top rejection reasons from learner_svc.rejections]"`.

Option C — Both: weighted retrieval (A) for passive reinforcement + quality summary (B) for explicit signal.

**Recommended:** Option C. Option A is a 2-line change in `db/queries.go`. Option B requires a new DB call in `_build_prompt` and a quality summary query in Learner's `QueryKnowledgeGraph` response or a new `GetMarketQualitySummary` gRPC method.

---

## MEDIA-01 — ALTO: Articles have no images

**Sections:** `05-agent-pipeline.md`, `06-infrastructure.md`
**Status:** Identified gap, Option C approved for v1

**Problem:** The agent pipeline generates text only. Sanity draft documents are created with no `heroImage` or inline media. Articles publish as text-only, which is unacceptable for wine and food journalism where visual quality directly affects reader trust and engagement.

**Options considered:**

| Option | Approach | Risk |
|--------|----------|------|
| A | AI-generated images via DALL-E/Stable Diffusion — new `generate_image` node in agent pipeline, image stored in S3/R2, URL attached to Sanity mutation | Wrong/uncanny images damage brand; copyright ambiguity on generated content |
| B | Stock photos via Unsplash/Pexels API — keywords extracted from article title, best match fetched automatically | Generic results; may not match specific wines/producers; API rate limits |
| C | Editor-assigned in Sanity Studio — draft lands without image, human editor selects before publishing | No automation; requires editor step on every article |

**Approved solution (v1):** Option C. Wine and food imagery is highly specific (correct bottle, correct producer, correct appellation) — wrong AI images are worse than no images and create editorial liability. Editors assign images in Sanity Studio before publishing. The publish action in Sanity Studio is already a manual step, so this adds no extra friction to the workflow.

**Future (v2):** Option B as a suggestion layer — auto-attach a stock photo as a placeholder in the Sanity draft, editors override if needed. Requires: Unsplash/Pexels API key in Vault, keyword extraction from article title, new field `suggestedImage` in Sanity document schema.

**What needs to happen now:** Sanity document schema (`_type: "article"`) must include a `heroImage` field of type `image`. The field is left empty by the connector. Sanity Studio validation should warn (not block) if `heroImage` is missing at publish time.

---

## OPS-02 — BASSO: Audit log not exposed in frontend

**Sections:** `07-auth.md`, `08-operations.md`
**Status:** Depends on PHASE4-02

**Problem:** Audit log exists in PostgreSQL and is accessible via `GET /api/admin/audit` but there is no UI for it.

**Approved solution:** Add an Audit Log page to the frontend (Phase 4), visible to admin role only. Paginated, filterable by event_type and market. No export button (security: audit data stays in the app).
