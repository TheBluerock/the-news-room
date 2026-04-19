# 06 — Infrastructure

## Local Development Stack

Start with: `make dev-up`

This starts the full stack via `docker-compose.dev.yml`:

| Container | Image | Ports | Purpose |
|-----------|-------|-------|---------|
| `postgres` | postgres:16 | 5432 | Primary database |
| `redis` | redis/redis-stack:7 | 6379 | Cache, HNSW, rate limiter |
| `redpanda` | redpandadata/redpanda:latest | 9092, 8081, 9644 | Kafka-compatible message bus + Schema Registry |
| `vault` | hashicorp/vault:1.17 | 8200 | Secrets management (dev mode) |
| `tempo` | grafana/tempo:latest | 4317, 3200 | OTel trace backend |
| `prometheus` | prom/prometheus:latest | 9090 | Metrics scraping |
| `grafana` | grafana/grafana:latest | 3000 | Dashboards |
| `auth` | (local build) | 8080, 8090 | Auth service |
| `learner` | (local build) | 8080, 8090 | Learner service |
| `agent` | (local build) | 8090 | Agent service |
| `moderation` | (local build) | 8090 | Moderation service |
| `correction` | (local build) | 8090 | Correction service |
| `analytics` | (local build) | 8080, 8090 | Analytics service |

### Startup Order

`make dev-up` waits for infrastructure before starting services:
1. PostgreSQL healthcheck passes
2. Redis ready
3. Vault dev mode seeded (`make vault-seed`)
4. Migrations applied (`make migrate-up ENV=local`)
5. RedPanda topics + schemas created (`make redpanda-setup`)
6. Services start

---

## Vault Integration

### Local Dev (Dev Mode)

Vault runs in dev mode — no authentication, all secrets at `secret/data/<service>`. Seeded by `make vault-seed`.

### Production Pattern (Swarm)

Each service container runs an entrypoint script that fetches secrets from Vault before starting the main process:

```bash
#!/bin/sh
# entrypoint.sh
set -e
VAULT_ADDR="${VAULT_ADDR:-http://vault:8200}"
ROLE="${VAULT_ROLE:-$SERVICE_NAME}"

# Authenticate with AppRole
VAULT_TOKEN=$(vault write -field=token auth/approle/login \
    role_id="${VAULT_ROLE_ID}" \
    secret_id="${VAULT_SECRET_ID}")
export VAULT_TOKEN

# Fetch secrets and write to /vault/secrets/<service>
mkdir -p /vault/secrets
vault kv get -format=json "secret/data/${ROLE}" \
    | jq -r '.data.data | to_entries[] | "\(.key)=\(.value)"' \
    > /vault/secrets/${ROLE}

# Exec the real service binary
exec "$@"
```

Services read from `/vault/secrets/<service>` as a key=value file. Never from environment variables.

> **K8s note:** In the original K8s design, the Vault Agent ran as a sidecar injecting secrets into a shared tmpfs. The Swarm entrypoint script is the equivalent pattern without a sidecar requirement.

### Secret Rotation

- PostgreSQL/Redis passwords: auto-rotated every 30 days by Vault dynamic secrets
- JWT RS256 keys: rotated via Vault PKI backend, 15-minute overlap window
- Services poll `/vault/secrets/` every 5 minutes and reload without restart

---

## Observability Stack

### OpenTelemetry

Every service initializes OTel at startup:

**Go:** `services/<name>/internal/telemetry/otel.go`
- TracerProvider → OTLP gRPC → Tempo (`:4317`)
- MeterProvider → Prometheus exporter
- Returns `shutdown func` and Prometheus `http.Handler`

**Python:** `services/<name>/telemetry.py`
- `OTLPSpanExporter` → Tempo (`:4317`)
- `BatchSpanProcessor`
- `FastAPIInstrumentor().instrument()` for automatic HTTP spans

### Prometheus Scrape Config

All services expose `/metrics` on port 8090. Prometheus scrapes via static config:

```yaml
scrape_configs:
  - job_name: newsroom-services
    static_configs:
      - targets:
          - auth:8090
          - learner:8090
          - agent:8090
          - moderation:8090
          - correction:8090
          - analytics:8090
```

### Grafana Provisioning

Auto-provisioned via volume mounts in `docker-compose.dev.yml`:

```
infra/grafana/provisioning/datasources/datasources.yaml  →  Prometheus + Tempo data sources
infra/grafana/provisioning/dashboards/dashboards.yaml    →  Dashboard provider config
infra/grafana/dashboards/newsroom.json                   →  Starter dashboard
```

**Datasources:**
- Prometheus (`uid: prometheus`) → `http://prometheus:9090`
- Tempo (`uid: tempo`) → `http://tempo:3200`

**Starter dashboard panels:**
- Service UP/DOWN (stat panels, one per service)
- Auth request rate + p99 latency
- Pipeline throughput (articles generated, approved, rejected per hour)
- LLM circuit breaker state per market
- Correction TTL remaining
- Tempo trace explorer (TraceQL)

### Key Prometheus Metrics

| Metric | Labels | Description |
|--------|--------|-------------|
| `llm_circuit_state` | `market`, `provider` | 0=CLOSED, 1=OPEN, 2=HALF_OPEN |
| `correction_ttl_remaining_seconds` | `market`, `correction_id` | Alert when < 3600 |
| `corrections_written_total` | `market` | Fast-path writes |
| `articles_generated_total` | `market` | Pipeline completions |
| `articles_approved_total` | `market` | Moderation pass |
| `articles_rejected_total` | `market`, `reason` | Moderation fail |

---

## Caddy (Reverse Proxy)

`infra/caddy/Caddyfile` — routes external traffic:

```
newsroom.local {
    reverse_proxy /api/auth/*   auth:8080
    reverse_proxy /api/*        frontend:3000
    reverse_proxy /grafana/*    grafana:3000
}
```

TLS termination via Caddy automatic HTTPS (Let's Encrypt in prod, self-signed in dev).

---

## Docker Swarm (Production Target)

> **v4.1 target:** Docker Swarm replaces Kubernetes. Lower ops overhead for current team size. Full Swarm stack files are pending (see `09-open-issues.md` REF-04).

**Planned stack files:**
- `infra/swarm/stack.dev.yml` — local single-node Swarm
- `infra/swarm/stack.prod.yml` — multi-node production Swarm

**Key Swarm constraints vs K8s:**
- No native Job primitive — use long-running consumers that process events and idle
- Vault sidecar → entrypoint script pattern (see above)
- Rolling updates via `docker service update --update-parallelism 1 --update-delay 30s`
- Health checks: `HEALTHCHECK` in Dockerfile pointing to `/health` on port 8090

**Deployment:**
```bash
docker stack deploy -c infra/swarm/stack.prod.yml newsroom
```

---

## RedPanda Setup

`infra/scripts/setup-redpanda.sh` creates all topics and registers schemas. Run automatically by `make dev-up` after RedPanda is healthy.

**Topic creation:**
```bash
rpk topic create topic.trending --partitions 3 --replicas 1
rpk topic create article.generated --partitions 3 --replicas 1
# ... all 7 topics + .dlq variants
```

**Schema registration:**
```bash
curl -X POST http://redpanda:8081/subjects/article.generated-value/versions \
  -H "Content-Type: application/vnd.schemaregistry.v1+json" \
  -d @infra/schemas/article.generated.json
```
