# Implementation Plan — Gap Closure to v4.1 Spec

Date: 2026-05-20
Source: audit of repo vs `CLAUDE.md` + `AI-Newsroom-v4.1.docx`.
Status legend: ☐ todo · ◐ partial · ✔ done.

---

## Phase A — Cleanup (1–2 days, no risk)

A1. ☐ Remove stale `.github/workflows/correction.yml` — service absorbed into learner (REF-01).
A2. ☐ Audit `frontend/app/` vs `frontend/src/` — delete leftover Next.js scaffold per memory `project_frontend_stack`.
A3. ☐ Resolve duplicate pipeline in `services/agent/`: `pipeline.py` vs `pipeline/` dir. Pick one, delete other.
A4. ☐ Deploy target = Docker Swarm. Update CLAUDE.md (remove "K8s-first" line). Mark `infra/helm/` deprecated or delete.

Exit criteria: `git grep -l correction-service` clean; one frontend tree; one pipeline module; one deploy story.

---

## Phase B — Helm chart completion (SKIP if Swarm)

Deferred. Deploy target = Swarm (see Phase C). Keep skeleton for future K8s migration or delete `infra/helm/` entirely. Decide in A4.

Original todos below kept for reference only:

B1. ☐ `templates/deployment.yaml` — per-service Deployment with vault-agent sidecar, tmpfs `/vault/secrets`.
B2. ☐ `templates/service.yaml` — ClusterIP per service, port 8090 for health, main port for gRPC/HTTP.
B3. ☐ `templates/ingress.yaml` or Caddy ConfigMap reference.
B4. ☐ `templates/configmap.yaml` — non-secret config per service.
B5. ☐ `templates/serviceaccount.yaml` + Vault K8s auth role bindings.
B6. ☐ `templates/hpa.yaml` — HPA on CPU + custom metric (RedPanda lag).
B7. ☐ `templates/networkpolicy.yaml` — restrict pod-to-pod by label.
B8. ☐ `templates/servicemonitor.yaml` — Prometheus scrape config.
B9. ☐ `values.yaml` + `values-staging.yaml` + `values-prod.yaml`.
B10. ☐ `helm lint` + `helm template` in CI (`.github/workflows/infra.yml`).

Exit: `helm install newsroom infra/helm/newsroom --dry-run` clean on all 3 value files.

---

## Phase C — Self-hosted bootstrap (3–5 days)

Target: Docker Swarm (already started in `infra/swarm/`). Drop Terraform. Delete empty `infra/terraform/`.

**Hosting plan** (AWS scartato):
- **Daily dev**: docker-compose locale Mac (`make dev-up`).
- **Staging**: Contabo VPS S — €5.50/mo, 4vCPU/8GB/200GB NVMe.
- **Prod**: Contabo VPS L — ~€15/mo, 8vCPU/30GB/800GB NVMe.
- Ansible playbook per Contabo (Debian 12 / Ubuntu 24.04).

C1. ☐ Ansible playbook `infra/ansible/bootstrap.yml`:
  - Install Docker + Swarm init on manager node.
  - Join workers via swarm token.
  - Sysctl tuning (vm.max_map_count for RedPanda, somaxconn).
  - UFW/iptables: open 2377 (swarm), 7946, 4789; deny rest.
  - Install node_exporter + promtail.
C2. ☐ Compose stacks consolidated:
  - `infra/swarm/stack.prod.yml` — all services + RedPanda + Postgres + Redis + Vault + Tempo + Grafana + Prometheus + Caddy.
  - Use Docker secrets (not env vars) for bootstrap creds; Vault takes over runtime.
  - Volumes on named host paths under `/srv/newsroom/{postgres,redis,redpanda,vault,tempo}`.
C3. ☐ Postgres: streaming replica on second node (`pg_basebackup` + `recovery.conf`). Document failover steps in `ops/DR.md`.
C4. ☐ Redis: AOF + RDB snapshots; replica on second node; HNSW requires RedisStack ≥7.2 image.
C5. ☐ RedPanda: 3-node cluster (min for quorum); rack awareness via labels.
C6. ☐ Vault: 3-node Raft cluster; auto-unseal via passphrase in HSM or KMS-equivalent (or manual unseal w/ documented runbook).
C7. ☐ Backup cron:
  - `pg_dump` nightly → Backblaze B2 (S3-compatible, cheap egress).
  - Vault snapshot daily → B2.
  - Audit log monthly archive → B2 con object lock 1y.
  - Restore test quarterly.
C8. ☐ Caddy auto-TLS via Let's Encrypt; config in `infra/caddy/Caddyfile`.
C9. ☐ CI: `ansible-lint` + `docker stack config` validate on PR.
C10. ☐ Deprecate Helm chart **or** keep for future K8s migration — decide and document in `infra/README.md`.

Exit: fresh VPS → `ansible-playbook bootstrap.yml` → `docker stack deploy` → smoke test green.

---

## Phase D — Test coverage (ongoing, 1–2 wks burst)

Current: 6 Go test files (jwt_test.go, enforcer_test.go, db_test.go, redis_test.go, http_test.go, plus the original), 2 Python test files. Spec: Go ≥75%, Py ≥80%.

D1. ☐ Go services — table tests per package:
  - ◐ `services/auth/internal/...` JWT issue/verify, Casbin policy, Redis blocklist — covered by this PR (jwt_test.go, enforcer_test.go, db_test.go, redis_test.go, http_test.go).
  - `services/learner/internal/...` scraper, embeddings batch, fastpath DEL, graph upsert.
  - `services/analytics/internal/...` trend detection, scoring, admin HTTP.
  - `services/sanity/internal/...` idempotency key reuse.
D2. ☐ Python services:
  - `services/agent/tests/` — pipeline node-by-node (mock LLM), circuit breaker state machine, semaphore limit, rate limit, prompt build per market.
  - `services/moderation/tests/` — extend cultural/factual checks, DLQ path, analytics quality score emit.
D3. ☐ CI gate: coverage fail under threshold per service workflow.
D4. ☐ Integration test: docker-compose stack + happy path (`topic.trending → article.published`) using k6 or pytest.
D5. ☐ Contract tests: schema produce/consume round-trip per topic in `infra/schemas/`.

Exit: coverage badges green; CI blocks PR on regression.

---

## Phase E — Observability completion (3–5 days)

E1. ☐ Confirm `/health` + `/ready` on **port 8090** for all 6 services (auth, learner, agent, moderation, analytics, sanity). Audit + add missing.
E2. ☐ Grafana dashboards as code in `infra/grafana/dashboards/`:
  - LLM circuit state (`llm_circuit_state{market,provider}`).
  - DLQ depth per topic.
  - Correction TTL remaining (alert <3600s).
  - Article pipeline trace latency p50/p95/p99 per market.
  - RedPanda consumer lag per group.
E3. ☐ Alertmanager rules:
  - DLQ depth >0 immediate.
  - Circuit open >5m.
  - Migration failure.
  - p99 latency +50% for 2m → auto-rollback hook.
E4. ☐ OTel trace ID propagation E2E test: assert single trace per article from `topic.trending` to `article.published`.

Exit: dashboards render with synthetic data; alerts fire in staging.

---

## Phase F — Ops runbooks (2 days)

Existing: `DEPLOY.md`, `DLQ-handling.md`, `DR.md`, `ROLLBACK.md`, `VAULT-DR.md`.

F1. ☐ `ops/LLM-CIRCUIT.md` — diagnose tripped breaker, force-close, fallback provider switch.
F2. ☐ `ops/CORRECTION-LAG.md` — when TTL alert fires; manual Learner replay.
F3. ☐ `ops/SCHEMA-REJECT.md` — Schema Registry rejection; produce blocked; upgrade path to vN+1.
F4. ☐ `ops/SECRETS-ROTATE.md` — manual rotation if Vault auto-rotate fails.

Exit: each runbook has decision tree + commands + escalation contact.

---

## Phase G — Proto + schema versioning (1 day)

G1. ☐ Confirm `proto/moderation.proto` + `proto/agent.proto` not needed (event-driven only) — document in `proto/README.md`.
G2. ☐ Add `buf breaking` check to CI on `proto/` path.
G3. ☐ Document schema version bump procedure in `infra/schemas/README.md` (e.g. `article.generated.v2.json` workflow).

---

## Phase H — CI/CD hardening (2–3 days)

H1. ☐ Each `.github/workflows/<service>.yml` gates: lint → test+coverage → buf → build → push → migrate → deploy staging → progressive prod rollout (5/50/100).
H2. ☐ Swarm-native progressive deploy via `docker service update` with `update_config`: `parallelism: 1`, `delay: 30s`, `order: start-first`, `monitor: 60s`, `failure_action: rollback`, `max_failure_ratio: 0.0`. Health gate per task uses the `/health` + `/ready` endpoints on port 8090 (see Phase E1) — failing readiness probe during `monitor` window triggers automatic Swarm rollback. Traffic shifting handled by Caddy upstream weights (config in `infra/caddy/Caddyfile`) updated by a deploy script that polls `/ready` + Prometheus error-rate / p99 latency before advancing 5%→50%→100%. Auto-rollback condition: error rate >1% or p99 latency delta >50% for 2m. Vault token handling unchanged (GitHub OIDC → Vault service token, see H3).
H3. ☐ Vault service token in GitHub OIDC, not long-lived secret.
H4. ☐ Image signing (cosign) + SBOM (syft) per build.
H5. ☐ Dependabot or Renovate on Go/Python/Helm.

---

## Sequencing

```
A (cleanup) ──► B (helm) ──► C (bootstrap) ──► H (CI/CD)
              └► D (tests) ──► E (observability) ──► F (runbooks)
                                                  └► G (proto/schema)
```

Parallelisable: D + E + F after B done. C blocks H prod canary.

## Effort estimate

| Phase | Days | Owner |
|-------|------|-------|
| A     | 1–2  | any   |
| B     | 3–5  | infra |
| C     | 3–5  | infra |
| D     | 5–10 | per-service owner |
| E     | 3–5  | infra + SRE |
| F     | 2    | SRE   |
| G     | 1    | platform |
| H     | 2–3  | infra |

Total critical path ≈ 4 weeks single-thread; ≈ 2.5 weeks with 2 streams.

## Done definition (v4.1 GA)

- All 6 services pass coverage threshold in CI.
- Fresh VPS → Ansible bootstrap → `docker stack deploy` → smoke test green.
- Synthetic article trace visible end-to-end in Tempo.
- DLQ + circuit breaker + correction TTL alerts firing in staging.
- Canary deploy auto-rolls back on injected failure.
- All ops runbooks reviewed in tabletop exercise.
