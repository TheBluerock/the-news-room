# 01 — Overview

## Purpose

AI Newsroom is an autonomous content generation platform for wine and food journalism. It targets three culturally distinct markets — Italy, USA, and China — and produces market-appropriate articles by learning from journalist profiles, editorial feedback, and real-time trending signals. Human editors remain in the loop before any article is published.

The system is designed for reliability, not speed. An article that takes 20 minutes to produce correctly is preferable to one that publishes incorrect information in 2 minutes.

## Core Principles

| Principle | Implementation |
|-----------|----------------|
| Reliability over cleverness | Explicit DLQ, retry backoff, circuit breakers — no silent failures |
| Strict separation of concerns | Each service owns one domain; cross-domain calls are gRPC or events, never shared DB |
| Event-driven async | RedPanda for all async communication; gRPC only for synchronous queries |
| Human in the loop | No article reaches Sanity without moderation approval + editor review |
| Nothing silently lost | DLQ on every consumer; depth > 0 triggers an immediate Grafana alert |
| Nothing in plaintext | HashiCorp Vault for all secrets; services read from `/vault/secrets/` filesystem |
| Everything traceable | W3C TraceContext on every event; one OTel trace per article, start to publish |

## Markets

| Market | Key | Cultural Persona |
|--------|-----|-----------------|
| Italy | `italy` | Formal register, DOC/DOCG appellations, regional terroir emphasis |
| USA | `usa` | Approachable, point-score driven (e.g. Wine Spectator scale), accessibility |
| China | `china` | Luxury framing, gifting context, brand prestige, import regulations awareness |

Market key is always lowercase. Every event payload, Redis key, and gRPC request includes the market key. Mixing market personas is a correctness bug, not a style preference.

## Scale Targets (v4.1)

| Metric | Target |
|--------|--------|
| Articles per market per day | ~10 (configurable via rate limiter) |
| Concurrent article generation per market | 2 (semaphore in agent service) |
| LLM calls per article | 3–5 (fetch context, generate, moderation checks) |
| Correction fast-path latency | < 1s Redis write |
| Correction slow-path latency | 1–6h PostgreSQL + HNSW re-index |
| End-to-end trace retention | 7 days (Grafana Tempo) |
| Audit log retention | 1 year, then S3 archive |

## Implementation Phases

| Phase | Description | Status |
|-------|-------------|--------|
| 1 | Foundation: services, Vault, DB migrations, gRPC proto | Complete |
| 2 | Observability: OTel tracing, Prometheus metrics, Grafana dashboards, RedPanda topics/schemas | Complete |
| 3 | Core AI pipeline: Agent LangGraph, Moderation checks, Analytics trends, Correction fast-path | Complete |
| 4 | Frontend & Sanity CMS: Next.js 15 editorial UI, i18n (IT/EN/ZH), Sanity webhook → `article.published` | Pending |
| 5 | CI/CD & Hardening: GitHub Actions per-service pipelines, Argo Rollouts canary, load testing | Pending |

> Phase 2 (Observability) is never skipped or deferred. Instrument before building AI features — blind LLM pipelines are undebuggable in production.

## Technology Choices

| Layer | Technology | Why |
|-------|-----------|-----|
| Go services | Go 1.25, pgx/v5, franz-go, OTel SDK | Type safety, low memory, first-class gRPC |
| Python services | Python 3.12, FastAPI, LangGraph, confluent-kafka | LLM ecosystem, async-native |
| Message bus | RedPanda (Kafka-compatible) | Schema Registry, DLQ, low ops overhead vs. full Kafka |
| Database | PostgreSQL 16 | ACID, pgvector for embeddings |
| Cache / vector index | Redis 7 (HNSW via RediSearch) | Fast corrections, token bucket, vector search |
| Secrets | HashiCorp Vault | Dynamic DB credentials, auto-rotation, audit trail |
| Tracing | Grafana Tempo + OTel | Distributed trace per article |
| Metrics | Prometheus + Grafana | Standard alerting, circuit breaker state dashboards |
| CMS | Sanity | Structured content, human editorial workflow |
| Container orchestration | Docker Swarm (dev + prod v4.1) | Lower ops than K8s for current team size |
