# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

AI Newsroom is an autonomous content generation platform for wine and food journalism targeting three markets: **Italy, USA, and China**. It learns from journalist profiles, generates culturally-appropriate articles, and improves through editorial feedback. This is v4.1 — an architecture-first project documented in `AI-Newsroom-v4.1.docx`.

Core philosophy: reliability over cleverness, strict separation of concerns, event-driven async communication, human-in-the-loop before publish, nothing silently lost (DLQ everywhere), nothing in plaintext (Vault for all secrets), everything traceable (OpenTelemetry end-to-end).

## Repository Layout

```
newsroom/
├── Makefile
├── docker-compose.dev.yml
├── services/
│   ├── auth/         # Golang — JWT RS256 + Casbin RBAC
│   ├── learner/      # Golang — scraping, knowledge graph, embeddings
│   ├── agent/        # Python — LangGraph article generation pipeline
│   ├── moderation/   # Python — cultural/factual checks, gating
│   ├── correction/   # Golang — fast path Redis corrections, 48h TTL
│   └── analytics/    # Golang — trend detection, quality scoring
├── frontend/         # Astro + Svelte, static-generated articles, i18n (IT/EN/ZH), hosted on Vercel. Admin UI is a separate app (not in this repo).
├── proto/            # Shared gRPC definitions
├── infra/
│   ├── terraform/
│   ├── helm/newsroom/        # Kubernetes Helm chart + Argo Rollouts config
│   ├── vault/                # Vault policies + Vault Agent sidecar config
│   ├── schemas/              # RedPanda Schema Registry (Avro/JSON Schema)
│   ├── migrations/postgres/  # golang-migrate SQL files (NNN_name.up/down.sql)
│   └── caddy/Caddyfile
├── cmd/dlq-tool/     # CLI: list, inspect, replay, discard DLQ messages
├── ops/              # DR.md, VAULT-DR.md, DLQ-handling.md, ROLLBACK.md
└── .github/workflows/ # Per-service CI/CD pipelines
```

## Development Commands

```bash
make dev-up                    # Start full local stack (all services + RedPanda + PostgreSQL + Redis + Vault + Tempo)
make vault-seed                # Seed local Vault dev mode with dummy credentials
make migrate-up ENV=local      # Apply all pending PostgreSQL migrations
make migrate-down ENV=local    # Roll back last PostgreSQL migration
make dlq-list                  # List all DLQ topics and pending message counts
make dlq-replay TOPIC=x        # Replay messages from a DLQ topic to the original topic
make load-test                 # Run k6 load test against staging
```

**Golang services** — `go test ./...` (with `-race`), `golangci-lint`, `go build`, coverage ≥ 75%  
**Python services** — `pytest`, `mypy`, `ruff`, coverage ≥ 80%  
**Migrations** — always run `make migrate-up` before deploying a new service version  
**Proto** — `buf lint` + breaking change detection on every PR touching `proto/`

## Architecture

### Communication
- **gRPC** for synchronous internal calls (Auth ↔ other services, Agent → Learner, Moderation → Learner)
- **RedPanda** (Kafka-compatible) for all async event-driven communication
- All RedPanda messages carry W3C TraceContext headers for OTel propagation
- All events are schema-validated via RedPanda Schema Registry (`infra/schemas/`) at both produce and consume time — malformed events are rejected at source

### Event Flow
```
topic.trending (Analytics → Agent)
  → article.generated (Agent → Moderation)
  → article.approved (Moderation → Sanity)
  → article.published (Sanity → Analytics)
editor.correction (Frontend → Learner + Correction Processor)  ← dual path
moderation.rejected (Moderation → Learner + Correction Processor)
```

### Secrets — HashiCorp Vault + Agent Sidecar
Every K8s pod runs a `vault-agent` sidecar that writes credentials to `/vault/secrets/` on a shared tmpfs volume. Services read secrets from the filesystem — never from env vars. Vault auto-rotates PostgreSQL/Redis passwords every 30 days without service restarts. JWT RS256 key rotation uses a 15-minute overlap window. Local dev uses Vault dev mode seeded by `make vault-seed`.

### Health Endpoints (every service)
All services expose on **port 8090** (independent of the main gRPC/HTTP port):
- `GET /health` — liveness: returns 200 if process is alive; never fails due to downstream deps
- `GET /ready` — readiness: returns 200 only when all deps (PostgreSQL, Redis, Vault) are connected and secrets loaded

### Redis (5 roles)
- **HNSW vector index** — article embeddings per market namespace (`vectors:italy:*`, `vectors:usa:*`, `vectors:china:*`), upgraded from FLAT, embeddings batched 100/call
- **Short-term memory** — recent topics/sources/angles per market (3–14 day TTL)
- **Fast-path corrections** — editor override signals injected directly into Agent prompts (48h TTL); Learner DELs the key after applying to PostgreSQL
- **JWT blocklist** — revoked tokens
- **Rate limiting** — token bucket per market preventing LLM budget explosions

### Agent Service — LangGraph Pipeline
Python/LangGraph pipeline per article: `fetch_memory → fetch_corrections → fetch_context (gRPC→Learner) → semantic_search (HNSW) → fetch_analytics → check_rate_limit → check_circuit → build_prompt → generate → write_memory → publish`. Max 2 concurrent runs per market (semaphore). Market-specific system prompts (Italy: formal/DOC-DOCG, USA: approachable/scores, China: luxury/gifting).

### LLM Circuit Breaker
Per-market circuit breaker on all LLM calls. Primary: OpenAI GPT-4o. Fallback: Anthropic `claude-sonnet-4-6`. Circuit trips after ≥5 failures in 60s; half-open probe after 30s. If both circuits open, article is queued with exponential backoff (60s → 5m → 30m) then DLQ. State exposed as `llm_circuit_state{market, provider}` Prometheus metric.

### Correction Fast Path vs. Slow Path
- **Fast path** (Correction Processor): writes to Redis immediately, 48h TTL, overrides Agent prompt on next run
- **Slow path** (Learner): updates PostgreSQL knowledge graph on schedule (1–6h), regenerates HNSW embeddings, then explicitly DELs Redis correction key
- Alert fires when `correction_ttl_remaining_seconds < 3600` — signals Learner is delayed

### Database Migrations
- Golang services: `golang-migrate`, SQL files in `infra/migrations/postgres/` named `NNN_description.up.sql` / `NNN_description.down.sql`
- Python services: Alembic, configured in `services/agent/alembic.ini`
- CI/CD gate: migrations run before deploying new service version; deploy is blocked if any migration fails
- All migrations must be backward-compatible for zero-downtime rolling deploys

### Audit Log (append-only)
`audit_log` table in PostgreSQL — no UPDATE or DELETE granted to any service role. Logs: article approvals/rejections, corrections, role changes, correction reversals. Exposed to Admin role only via `GET /api/admin/audit`. Retained 1 year, then archived to S3.

### CI/CD — GitHub Actions + Argo Rollouts
Per-service workflow files in `.github/workflows/` with path filtering. Pipeline: PR → tests only; merge to main → test → build → push image → run migrations → deploy staging (auto); staging verified → canary to production (5% → 50% → 100% over ~10 min). Auto-rollback if error rate >1% OR p99 latency increases >50% for 2 minutes. Manual rollback: `helm rollback newsroom [revision]` — full procedure in `ops/ROLLBACK.md`.

### DLQ Strategy
Every RedPanda consumer topic has a corresponding `*.dlq` topic. Retry backoff: 5s → 30s → 2m → DLQ. DLQ depth >0 triggers immediate Grafana alert. Use `cmd/dlq-tool` CLI for inspection and replay. Runbook: `ops/DLQ-handling.md`.

## Implementation Order

Phase 1 (Foundation) → Phase 2 (Observability — **never skip, instrument before building AI**) → Phase 3 (Core AI Pipeline) → Phase 4 (Frontend & Analytics) → Phase 5 (CI/CD & Hardening)

Deployment is microservices-first on Kubernetes from Phase 1 — each service has its own Dockerfile and CI pipeline. Do not build a monolith first.

## Key Constraints

- **OTel spans**: one trace ID per article, covering the full pipeline from trigger to publish. Golang: `go.opentelemetry.io/otel`. Python: `opentelemetry-sdk` + `opentelemetry-instrumentation-langchain`. Backend: Grafana Tempo.
- **Sanity idempotency**: always use `article.id` as idempotency key on every Sanity API call to prevent duplicates on replay.
- **Schema changes**: breaking RedPanda schema changes require a new event version (e.g. `article.generated.v2`). Schema compatibility is checked on every PR touching `infra/schemas/`.
- **GDPR**: `DELETE /api/user/data` publishes `user.data.deletion.requested`; all services must anonymise within 30 days.
- **Vault in GitHub Actions**: only the Vault service token is stored in GitHub Secrets; all actual credentials are fetched from Vault at deploy time.
