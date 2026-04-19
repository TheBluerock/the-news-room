# 08 — Operations

## Database Migrations

**Tool:** `golang-migrate` for Go services, Alembic for Python services.

**File naming:** `infra/migrations/postgres/NNN_description.up.sql` / `NNN_description.down.sql`

**Run migrations:**
```bash
make migrate-up ENV=local     # Apply all pending
make migrate-down ENV=local   # Roll back last migration
```

**CI/CD gate:** Migrations run before deploying a new service version. If any migration fails, the deploy is blocked. Services must be backward-compatible with both the old and new schema during the rolling window.

**Backward-compatible migration checklist:**
- Adding a nullable column: safe
- Adding a NOT NULL column: requires a default or a backfill migration first
- Renaming a column: requires a two-step migration (add new + copy + drop old)
- Dropping a column: requires removing all references in code first, then a follow-up migration
- Adding an index: CONCURRENTLY in production to avoid table lock

---

## DLQ Runbook

**Check DLQ depth:**
```bash
make dlq-list
# Output: topic → pending count
```

**Inspect messages:**
```bash
./cmd/dlq-tool/dlq-tool inspect --topic article.generated.dlq --limit 10
```

**Replay to original topic:**
```bash
make dlq-replay TOPIC=article.generated.dlq
# Replays all messages to article.generated
```

**Discard messages (last resort):**
```bash
./cmd/dlq-tool/dlq-tool discard --topic article.generated.dlq --reason "schema incompatibility in v4.0, superseded"
```

> Never discard without first inspecting. Discarded messages are written to the audit log before deletion.

**Full runbook:** `ops/DLQ-handling.md`

### Common DLQ Causes

| DLQ topic | Common cause | Resolution |
|-----------|-------------|------------|
| `article.generated.dlq` | LLM circuit open, rate limit exhausted | Wait for circuit reset, then replay |
| `article.generated.dlq` | Schema validation failure | Fix producer schema, discard old messages |
| `editor.correction.dlq` | Redis unavailable during correction write | Restore Redis, replay |
| `moderation.rejected.dlq` | Same as above | Restore Redis, replay |

---

## Rollback Procedure

**Full procedure:** `ops/ROLLBACK.md`

### Service Rollback (Docker Swarm)

```bash
# List service update history
docker service ps newsroom_agent --no-trunc

# Roll back to previous image
docker service rollback newsroom_agent
```

### Helm Rollback (K8s legacy)

```bash
helm rollback newsroom [revision]
helm history newsroom  # list revisions
```

### Migration Rollback

```bash
make migrate-down ENV=staging   # rolls back one migration
# Verify application still works
# Then redeploy previous service version
```

> Rolling back a migration while the new service version is running will cause undefined behavior. Always roll back the service first, then the migration.

---

## Alerting Thresholds

| Alert | Condition | Severity | Action |
|-------|-----------|----------|--------|
| DLQ depth | Any DLQ topic depth > 0 | Critical | Check DLQ runbook immediately |
| Circuit breaker open | `llm_circuit_state{provider="openai"} == 1` for > 5m | Warning | Check OpenAI API status |
| Both circuits open | All providers open for any market | Critical | Articles queuing; check LLM APIs |
| Correction TTL warning | `correction_ttl_remaining_seconds < 43200` (12h) | Warning | Learner slow path is running behind; check learner consumer lag |
| Correction TTL critical | `correction_ttl_remaining_seconds < 3600` (1h) | Critical | Correction about to expire before slow path applies; investigate immediately |
| Service down | `/ready` returns 503 for > 2m | Critical | Check logs, restart if needed |
| p99 latency increase | +50% increase for > 2m | Warning | Check OTel traces in Grafana Tempo |

---

## Audit Log Maintenance

The `audit_log` table is append-only. No service role has UPDATE or DELETE. Accessible via `GET /api/admin/audit` (admin role only).

**Archive to S3 (run annually):**
```sql
-- Export records older than 1 year
COPY (
    SELECT * FROM audit_log
    WHERE created_at < now() - interval '1 year'
) TO '/tmp/audit_archive_YEAR.csv' CSV HEADER;

-- After confirming S3 upload:
DELETE FROM audit_log
WHERE created_at < now() - interval '1 year';
```

> Only a database administrator (not any service role) can run the DELETE above. Service roles only have INSERT on `audit_log`.

---

## Correction Fast-Path Monitoring

Alert when `correction_ttl_remaining_seconds < 3600` — this means an editor correction is less than 1 hour from expiring and the Learner slow path hasn't applied it to PostgreSQL yet.

**Diagnose:**
```bash
# Check Learner consumer lag
make dlq-list
# Look for editor.correction.dlq depth

# Check Redis directly
redis-cli --scan --pattern "corrections:*" | head -20
redis-cli ttl "corrections:italy:<correction_id>"
```

**Resolution:** Usually Learner is lagging. Check Learner logs for slow queries or HNSW re-index failures. If Learner is stuck, the fast-path correction will remain in Redis until TTL expiry — articles continue using the correction, just without the slow-path persistence.

---

## Disaster Recovery

**Full DR runbook:** `ops/DR.md`

**Vault DR:** `ops/VAULT-DR.md` — covers Vault unsealing, key recovery, and re-seeding from backup.

**PostgreSQL backup:** pg_dump daily to S3. Point-in-time recovery via WAL archiving.

**Redis backup:** RDB snapshots every 6 hours to S3. Redis is a cache — losing it means cold start (embeddings must be re-indexed from PostgreSQL, circuit breakers reset to CLOSED, rate limiters reset to full).

**RedPanda backup:** Topic replication with `rf=3` in production. Consumer group offsets are committed after successful processing — replay from committed offset on consumer restart.

---

## Load Testing

```bash
make load-test   # runs k6 against staging
```

Scenarios in `tests/load/`:
- `article_generation.js` — trigger 50 concurrent `topic.trending` events across 3 markets
- `auth_validation.js` — token validation throughput
- `correction_fast_path.js` — editor correction latency under load

**Target SLOs:**
- Auth token validation p99 < 50ms
- Correction fast-path write p99 < 100ms
- Article pipeline end-to-end p95 < 3 minutes
