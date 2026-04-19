# AI NEWSROOM v4.1 — Documentation Index

**Version:** 1.0 | **Updated:** 2026-04-18

This directory is the single source of truth for the AI Newsroom platform.
Each file covers one domain. Load only what you need for the task at hand.

## File Map

| File | Load when you're working on... |
|------|-------------------------------|
| [`01-overview.md`](./01-overview.md) | Purpose, principles, markets, scale targets, phase plan |
| [`02-services.md`](./02-services.md) | Service boundaries, responsibilities, inter-service contracts |
| [`03-event-flow.md`](./03-event-flow.md) | RedPanda topics, schemas, DLQ strategy, full event chain |
| [`04-data-models.md`](./04-data-models.md) | PostgreSQL schemas, Redis key taxonomy, Vault secret paths |
| [`05-agent-pipeline.md`](./05-agent-pipeline.md) | LangGraph pipeline, circuit breaker, rate limiter, market personalities |
| [`06-infrastructure.md`](./06-infrastructure.md) | Docker Swarm/Compose, Vault entrypoint, Caddy, Grafana/Tempo |
| [`07-auth.md`](./07-auth.md) | JWT RS256, Casbin RBAC, roles, market scoping, token lifecycle |
| [`08-operations.md`](./08-operations.md) | Migrations, DLQ runbooks, rollback procedures, alerting |
| [`09-open-issues.md`](./09-open-issues.md) | All pending refactor items with priority IDs and approved solutions |

## Quick Reference

**Article pipeline:** `topic.trending` → `article.generated` → `article.approved` → `article.published` → Analytics feedback loop

**Correction dual-path:** `editor.correction` → Redis fast-path (48h TTL, immediate agent override) AND Learner slow-path (PostgreSQL + HNSW re-index, 1–6h)

**LLM fallback:** OpenAI GPT-4o (primary) → Anthropic claude-sonnet-4-6 (fallback) → queue with exponential backoff → DLQ

**Markets:** Italy (`italy`), USA (`usa`), China (`china`) — each has a distinct cultural persona; never mix market prompts

**Key invariants:**
- Secrets are always read from `/vault/secrets/` — never from environment variables
- Every Redpanda consumer has a corresponding `.dlq` topic — nothing is silently dropped
- Every article event carries a W3C TraceContext header for end-to-end OTel propagation
- Sanity idempotency key is always `article.id` — prevents duplicates on DLQ replay
- All health checks live on port 8090 — `/health` (liveness), `/ready` (readiness), `/metrics` (Prometheus)
- Agent max 2 concurrent article runs per market (semaphore-guarded)
- Database migrations run before deploying a new service version — deploy is blocked on failure
