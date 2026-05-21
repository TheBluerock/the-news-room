# Implementation Plan — Gap Closure to v4.1 Spec

Date: 2026-05-20
Source: audit of repo vs `CLAUDE.md` + `AI-Newsroom-v4.1.docx`.
Status legend: ☐ todo · ◐ partial · ✔ done.

---

## Phase A — Cleanup (DONE 2026-05-21)

A1. ✔ `.github/workflows/correction.yml` — kept as intentional tombstone (workflow_dispatch only, prevents accidental recreation; explains REF-01).
A2. ✔ `frontend/app/` deleted (was empty Next.js scaffold residue).
A3. ✔ `services/agent/pipeline.py` → `graph.py` (done during Phase D; consumer.py import updated).
A4. ✔ Deploy target = Docker Swarm:
  - CLAUDE.md updated (3 lines: layout, Vault sidecar, deployment paragraph).
  - `infra/helm/DEPRECATED.md` added explaining status + revival steps.
  - `infra/terraform/` empty dir removed; `.gitignore` lines pruned.
  - `.github/workflows/infra.yml` rewritten: terraform-plan + helm-lint jobs removed, swarm-config validate added.

**Exit criteria met:**
- `git grep correction-service` → only docs/tombstone.
- Single frontend tree (`frontend/src/`).
- Single pipeline module (`graph.py`).
- Single deploy story (Swarm; Helm dormant + documented).

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

## Phase C — Self-hosted bootstrap (PARTIAL — single-node done 2026-05-21; multi-node deferred)

Target: Docker Swarm (already started in `infra/swarm/`). Drop Terraform. Delete empty `infra/terraform/`.

**Hosting plan** (AWS scartato):
- **Daily dev**: docker-compose locale Mac (`make dev-up`).
- **Staging**: Contabo VPS S — €5.50/mo, 4vCPU/8GB/200GB NVMe.
- **Prod**: Contabo VPS L — ~€15/mo, 8vCPU/30GB/800GB NVMe.
- Ansible playbook per Contabo (Debian 12 / Ubuntu 24.04).

C1. ✔ Ansible playbook `infra/ansible/bootstrap.yml`:
  - Docker install via official apt repo (pinned version).
  - Swarm init on first manager; idempotent join for additional managers + workers.
  - Sysctl tuning (vm.max_map_count, somaxconn, port range, tcp_tw_reuse).
  - UFW: deny incoming default + allow SSH (rate-limited) + 80/443 + per-peer swarm ports.
  - node_exporter + cAdvisor as standalone containers (always-up).
  - Roles: common, firewall, docker, observability, swarm_manager, swarm_join_manager, swarm_join_worker.
  - Syntax check passes; `requirements.yml` declares collections.
C2. ◐ Stack `infra/swarm/stack.prod.yml` already consolidated (480→520 lines). Added: `backup` service, `b2_application_key_id/key`, `vault_token` secrets, `backup_state` volume, `backup_script` config. Volumes still on Docker-managed paths; pinning to `/srv/newsroom/*` deferred (cosmetic).
C3. ☐ **Deferred to multi-node phase.** Postgres replica needs second VPS; single-node staging OK initially.
C4. ☐ **Deferred to multi-node phase.** Redis replica + AOF: AOF can be enabled in stack today (one-line), replica needs second VPS.
C5. ☐ **Deferred to multi-node phase.** RedPanda 3-node quorum needs 3 VPS.
C6. ☐ **Deferred to multi-node phase.** Vault 3-node Raft needs 3 VPS; staging uses dev-mode unseal.
C7. ✔ Backup cron service:
  - `infra/scripts/run-backup.sh` — pg_dump | vault snapshot | audit_log monthly.
  - Crontab baked into container entrypoint: 03:30 daily PG, 03:45 daily Vault, 04:00 monthly audit.
  - Upload via rclone → Backblaze B2 (S3-compat).
  - ShellCheck-clean.
C8. ◐ Caddy auto-TLS — Caddyfile present in `infra/caddy/`; verify it issues Let's Encrypt certs on first deploy (manual test post-VPS).
C9. ✔ CI `.github/workflows/infra.yml`:
  - `swarm-config` — `docker compose config` validates stack files.
  - `ansible-lint` — syntax check + `ansible-lint --profile basic`.
  - `backup-script-shellcheck` — runs shellcheck on `infra/scripts/`.
C10. ✔ Helm chart deprecated (`infra/helm/DEPRECATED.md` done in Phase A).

**Files added:** `infra/ansible/{ansible.cfg, bootstrap.yml, inventory.example.ini, requirements.yml, group_vars/all.yml.example, roles/{common,firewall,docker,observability,swarm_manager,swarm_join_manager,swarm_join_worker}/tasks/main.yml}`, `infra/scripts/run-backup.sh`, `infra/README.md`.

**What's deferred (needs real VPS to test):** multi-node HA (PG replica, Redis replica, RedPanda 3-node, Vault 3-node Raft), restore drill rehearsal, Caddy LE issuance verification.

**Exit:** ready to run on fresh Contabo VPS S → single-node staging fully functional. Multi-node prod hardening = follow-up after first staging deploy validates the playbook.

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

## Phase I — Web analytics integration (2–3 days, deferred)

Goal: close feedback loop — real reader behavior (page views, dwell time, bounce) feeds into `analytics_svc.article_performance.quality_score`, which agent prompts read as quality signal.

Note: Phase I is the last phase and runs after Phase H. Kept lettered "I" (not renamed) so cross-references in code/comments referencing Phase I remain valid.

**Decision (2026-05-20):** Use **Plausible self-hosted**, NOT Google Analytics.

**Why Plausible over GA4:**
- Italian DPA (Garante) declared GA4 incompatible with GDPR 2022–2024. Frontend serves IT → blocker.
- No cookie banner needed (Plausible is cookieless) → ~40% more data captured (no consent-blocked users).
- Already self-hosted ethos (Contabo VPS).
- Real-time stats API (vs GA4 24–48h aggregation delay).
- ~200MB RAM, single Docker container — fits on same Contabo VPS S.
- Vendor neutral, no Google lock-in.
- Free self-hosted, no quota.

**Alternatives considered:**
- Umami (similar profile, less mature API).
- PostHog (richer feature-flag layer, heavier).
- GA4 + `cloud.google.com/go/analyticsdata/apiv1beta` — fallback only if a client mandates GA4.

I1. ☐ Add Plausible to `infra/swarm/stack.{dev,prod}.yml` (image: `plausible/analytics:v2.1.4` — pinned for reproducible deploys, bump via PR after staging validation; ~200MB RAM, port 8000 internal, behind Caddy at `analytics.newsroom.dev`).
I2. ☐ Provision Postgres + Clickhouse for Plausible (or share existing Postgres with new DB).
I3. ☐ Frontend (Astro): add Plausible script tag, configure per-market site IDs.
I4. ☐ New Go module `services/analytics/internal/plausible/fetcher.go`:
  - `FetchPageViews(ctx, siteID, dateRange) → map[articleSlug]int`
  - `FetchDwellTime(ctx, siteID, dateRange) → map[articleSlug]float64`
  - HTTP GET on `/api/v1/stats/breakdown` — no SDK needed, `net/http` + JSON decode.
I5. ☐ Cron loop in `services/analytics/cmd/main.go`: fetch hourly, upsert into `analytics_svc.article_performance` (add columns `view_count INT`, `avg_dwell_seconds FLOAT`).
I6. ☐ Migration: `infra/migrations/postgres/NNN_article_performance_engagement.up.sql` — add columns + index on view_count.
I7. ☐ Update `db.GetMarketQualitySummary` (learner) to blend Plausible signal into `avg_quality_score` calculation. Weight: 0.6 LLM heuristic + 0.4 reader engagement.
  - **Cross-service dependency:** this task modifies code in `services/learner/`, not analytics. Coordinate the PR with the learner owner; do not merge analytics-side changes (I5/I6) until learner-side blend logic is reviewed and merged. Document the blended-score contract in `services/learner/internal/db/README.md` so a future analytics change cannot silently break learner behavior.
I8. ☐ Test coverage:
  - Analytics side: testcontainers Plausible mock via httptest; integration test for the hourly cron loop end-to-end.
  - Learner side: extend `services/learner/internal/db/*_test.go` to cover `GetMarketQualitySummary` blend formula across boundary inputs (engagement absent / zero / saturated) — required before merging I7.
  - CI gate: learner workflow (`.github/workflows/learner.yml`) must run on any PR touching either `services/learner/**` or `services/analytics/internal/plausible/**`, so a cross-service regression is caught before merge.
I9. ☐ Dashboard panel "Engagement per market" in `infra/grafana/dashboards/`.
I10. ☐ Document Plausible URL + per-market site IDs in Vault path `secret/analytics/plausible/*`.

**Trigger to start:** after Phase D done + Plausible decision validated with at least one published article in prod (need real traffic to test fetch). Estimated 2–3 days work.

**Fallback to GA4:** if Plausible self-hosted proves heavy on Contabo VPS S, or if a client explicitly requires GA4 integration:
- Add `cloud.google.com/go/analyticsdata/apiv1beta` to `services/analytics/go.mod`.
- Service Account JSON in Vault path `secret/analytics/ga4_service_account`.
- Same fetcher interface (`FetchPageViews`, `FetchDwellTime`) — only implementation swaps.
- Note: GDPR consent banner required for EU traffic → ~40% data loss.

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
| I     | 2–3  | analytics + frontend (deferred, post-Phase D) |

Total critical path ≈ 4 weeks single-thread; ≈ 2.5 weeks with 2 streams.

## Done definition (v4.1 GA)

- All 6 services pass coverage threshold in CI.
- Fresh VPS → Ansible bootstrap → `docker stack deploy` → smoke test green.
- Synthetic article trace visible end-to-end in Tempo.
- DLQ + circuit breaker + correction TTL alerts firing in staging.
- Canary deploy auto-rolls back on injected failure.
- All ops runbooks reviewed in tabletop exercise.
