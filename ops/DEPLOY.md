# Deployment Guide

End-to-end reference for local dev and production (Docker Swarm).
Related docs: `ops/ROLLBACK.md`, `ops/VAULT-DR.md`, `ops/DLQ-handling.md`.

---

## Prerequisites

| Tool | Version | Purpose |
|------|---------|---------|
| Docker + Docker Compose | 26+ | Local dev stack |
| Docker Swarm | (same daemon) | Production deployment |
| `go` | 1.22+ | Running migrations |
| `vault` CLI | 1.16+ | Secret inspection and seeding |
| `rpk` | latest | RedPanda topic management |
| `k6` | 0.51+ | Load testing |
| `buf` | 1.x | Proto linting and generation |

---

## Local Development

### 1. Start the full stack

```bash
make dev-up
```

This runs in order:
1. Starts infrastructure: PostgreSQL, Redis, RedPanda, Vault (dev mode), Tempo, Prometheus, Grafana
2. Seeds Vault with dummy credentials (`infra/vault/scripts/seed.sh`)
3. Runs all PostgreSQL migrations (`infra/migrations/postgres/`)
4. Creates RedPanda topics and registers schemas
5. Starts all application services

### 2. Verify health

All services expose `/health` on port 8090:

```bash
curl http://localhost:8090/health   # moderation
curl http://localhost:8092/health   # agent-italy
curl http://localhost:8093/health   # auth
```

Docker Compose health checks run automatically — `make dev-up` waits for all services to be healthy.

### 3. Tear down

```bash
make dev-down   # stops containers AND removes volumes (wipes DB and Redis)
```

---

## Secrets — HashiCorp Vault

### Local (dev mode)

Vault runs in dev mode with root token `dev-root-token`. Seed script writes all secrets:

```bash
make vault-seed
```

### Production

Each container starts with a Vault AppRole `role_id` and `secret_id` injected as Docker Swarm secrets (at `/run/secrets/vault_role_id` and `/run/secrets/vault_secret_id`). The entrypoint script (`infra/docker/entrypoint.sh`) authenticates with AppRole and writes all service credentials to `/vault/secrets/` before the service binary starts.

Vault auto-rotates PostgreSQL and Redis passwords every 30 days. JWT RS256 keys rotate with a 15-minute overlap window.

> **Never** store actual credentials in environment variables, `.env` files, or GitHub Secrets. Only `VAULT_ADDR` and SSH deploy keys go in GitHub Secrets. All service credentials come from Vault.

### Create Swarm secrets (one-time, on manager node)

```bash
# Per service: vault_role_id_<service> and vault_secret_id_<service>
echo "<role_id>"   | docker secret create vault_role_id_auth -
echo "<secret_id>" | docker secret create vault_secret_id_auth -
# Repeat for: learner, agent, moderation, analytics, sanity

# Infrastructure secrets
echo "<password>" | docker secret create postgres_password -
echo "<password>" | docker secret create grafana_admin_password -
```

---

## Database Migrations

Migrations live in `infra/migrations/postgres/` named `NNN_description.up.sql` / `NNN_description.down.sql`.

```bash
make migrate-up ENV=local   # local dev
make migrate-up ENV=prod    # production (requires PROD_DB_URL env var)
make migrate-down ENV=prod  # roll back one migration
```

**CI gate**: migrations run before deploying any new service version. Deploy is blocked if any migration fails. All migrations must be backward-compatible for zero-downtime rolling updates.

---

## RedPanda Topics and Schemas

```bash
make redpanda-setup
```

Creates all topics (including `.dlq` variants) and registers schemas in the Schema Registry. Schema compatibility is enforced — breaking changes require a new event version (e.g. `article.generated.v2`).

---

## Building Images

```bash
make build-all TAG=<git-sha>
```

Images (pushed to your registry at `$REGISTRY`):

```
$REGISTRY/newsroom/auth:<tag>
$REGISTRY/newsroom/learner-server:<tag>
$REGISTRY/newsroom/learner-ingest:<tag>
$REGISTRY/newsroom/agent:<tag>           # single image — AGENT_MARKET env differentiates
$REGISTRY/newsroom/moderation:<tag>
$REGISTRY/newsroom/analytics:<tag>
$REGISTRY/newsroom/sanity:<tag>
```

In CI, images are built and pushed to GHCR (`ghcr.io/<org>/newsroom/<service>:<sha>`) on merge to `main`.

---

## Production Deployment (Docker Swarm)

### Node labelling (one-time)

```bash
# Infrastructure node (postgres, redis, redpanda, vault, caddy)
docker node update --label-add role=infra <node-id>

# Application nodes
docker node update --label-add role=app <node-id>
```

### Initial stack deploy

```bash
export REGISTRY=ghcr.io/<org>
export TAG=<git-sha>
export VAULT_ADDR=https://vault.example.com

make migrate-up ENV=prod   # always run migrations first
make swarm-deploy ENV=prod STACK=newsroom
```

This runs `docker stack deploy -c infra/swarm/stack.prod.yml newsroom --with-registry-auth`.

### Rolling update (single service)

```bash
docker service update \
  --image ghcr.io/<org>/newsroom/moderation:<new-sha> \
  --update-order start-first \
  newsroom_moderation
```

Swarm's update config (`start-first`, `failure_action: rollback`, 10s delay between replicas) handles zero-downtime rolling updates automatically.

### Check stack health

```bash
make swarm-status                                    # list all services and replica counts
docker service ps newsroom_moderation               # per-service task history
docker service logs newsroom_moderation --tail 100  # recent logs
```

### CI/CD — GitHub Actions

On merge to `main`, each service's workflow:
1. Runs tests
2. Builds and pushes image to GHCR
3. SSHes into the Swarm manager node, runs migrations, then `docker service update`

Required GitHub Secrets per repo:
```
SWARM_HOST       — manager node IP or hostname
SWARM_USER       — SSH user (must be in docker group)
SWARM_SSH_KEY    — private SSH key
VAULT_ADDR       — production Vault address
REGISTRY         — container registry prefix
```

---

## Service Port Reference

| Service | Main port | Health port |
|---------|-----------|-------------|
| auth | 8080 (HTTP), 8090 (gRPC) | 8090 |
| learner-server | 8082 (gRPC), 8088 (REST) | 8091 |
| learner-ingest | — | 8101 |
| agent-italy | 8083 | 8092 |
| agent-usa | 8097 | 8098 |
| agent-china | 8099 | 8100 |
| moderation | 8084 (HTTP+gRPC) | 8093 |
| analytics | 8086 (gRPC), 8096 (REST) | 8095 |
| sanity | 8087 | 8097 |
| admin UI | 4000 | 4000/api/health |
| Grafana | 3001 | — |
| Prometheus | 9090 | — |
| RedPanda | 9092 (Kafka), 8081 (Schema Registry) | 9644 (Admin) |
| Vault | 8200 | — |
| Tempo | 4317 (OTLP gRPC), 4318 (OTLP HTTP), 3200 (Query) | — |

Ports above reflect local dev. In production all traffic enters through Caddy (ports 80/443) on the infra node; service ports are internal to the overlay network only.

---

## Observability

- **Traces**: Grafana Tempo. All services emit OTLP traces. One trace ID per article covers the full pipeline (trigger → publish).
- **Metrics**: Prometheus + Grafana dashboards (admin / admin locally).
- **Logs**: JSON structured logs on stdout, collected by Docker.

### Key metrics to watch

```
llm_circuit_state{market, provider}                # 0=closed, 1=open, 2=half-open
correction_ttl_remaining_seconds{market}           # alert if < 3600
learner_correction_pg_write_failures_total{market} # alert if > 0
llm_tokens_total{market, model, type}              # token spend by market
llm_cost_usd_total{market, model}                  # cumulative cost
article_quality_per_1k_tokens{market, model}       # efficiency signal
```

---

## DLQ Monitoring

```bash
make dlq-list                                       # all DLQ topics + pending counts
make dlq-replay TOPIC=article.generated.dlq
make dlq-discard TOPIC=article.generated.dlq        # drop messages (irreversible)
```

DLQ depth > 0 triggers a Grafana alert. Runbook: `ops/DLQ-handling.md`.

---

## Load Testing

```bash
make load-test
# Override target:
BASE_URL=https://newsroom.example k6 run infra/load-test/k6-script.js
```

Thresholds: p99 < 2s, error rate < 1%, moderation queue p95 < 500ms.

---

## Proto Changes

```bash
buf lint
buf breaking --against '.git#branch=main'
make proto   # regenerate Go + Python stubs
```

Breaking schema changes require a new event version. CI blocks PRs with breaking proto changes.

---

## Common Issues

**`make dev-up` fails on Vault health check**
Vault dev mode needs a few seconds. Re-run `make dev-up` — it's idempotent.

**Agent circuit breaker tripped**
Check `llm_circuit_state` metric. If both GPT-4o and claude-sonnet-4-6 circuits open, articles queue with exponential backoff then DLQ. Verify OpenAI/Anthropic API keys in Vault.

**Correction fast-path missing from agent prompt**
Check `learner_correction_pg_write_failures_total` > 0 — PostgreSQL write failing silently. Check PG connectivity from learner-ingest container.

**Swarm service stuck in update**
```bash
docker service update --rollback newsroom_<service>
```
