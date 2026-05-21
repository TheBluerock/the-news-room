# Runbook — GDPR Deletion Lag

**Trigger**: Alertmanager `GDPRDeletionLag` (metric `gdpr_user_deletions_pending_total > 0`)

**Legal context**: GDPR Article 17 right to erasure — 30 days from request. Owner decision 2026-05-21: worldwide uniform, all markets. Soft alert at day 25 gives 5 days to remediate before legal cutoff.

## Diagnose

Connect to the auth Postgres and list pending rows:

```sql
SELECT user_id, requested_at, services_completed,
       EXTRACT(EPOCH FROM (now() - requested_at)) / 86400 AS days_old
FROM user_deletions
WHERE completed_at IS NULL
ORDER BY requested_at ASC;
```

For each row identify which service has not yet stamped in. Expected keys
(see `services/auth/internal/server/http.go ExpectedDeletionServices`):
- `auth` — written synchronously by the DELETE handler. Missing = handler crashed mid-write; check auth logs around `requested_at`.
- `analytics` — written by `services/analytics/internal/gdpr/consumer.go` on event consume. Missing = consumer group `analytics-gdpr` behind, or event never reached Kafka.

## Remediate

### Case A: `auth` not stamped

1. Verify the local anonymisation actually happened:
   ```sql
   SELECT id, email, password_hash, deleted_at
   FROM users WHERE id = '<user_id>';
   ```
   If email is still original → handler ledger row exists but anonymisation rolled back. Replay manually:
   ```sql
   BEGIN;
   UPDATE users SET email = 'deleted-' || id::text || '@anonymised.local',
                    password_hash = '', deleted_at = now()
   WHERE id = '<user_id>';
   UPDATE audit_log SET user_id = '00000000-0000-0000-0000-000000000001'
   WHERE user_id = '<user_id>';
   UPDATE user_deletions
   SET services_completed = services_completed || jsonb_build_object('auth', to_jsonb(now()))
   WHERE user_id = '<user_id>';
   COMMIT;
   ```

### Case B: `analytics` not stamped

1. Check the consumer group is alive:
   ```sh
   rpk group describe analytics-gdpr
   ```
   Lag should be 0 with active members.

2. If event never reached Kafka (auth handler crashed before publish), republish manually:
   ```sh
   docker exec -it newsroom_redpanda rpk topic produce user.data.deletion.requested <<EOF
   {"event_id":"$(uuidgen)","trace_id":"manual","user_id":"<user_id>","requested_by":"<user_id>","timestamp":"$(date -u +%FT%TZ)"}
   EOF
   ```

3. If event is in DLQ (`user.data.deletion.requested.dlq`), inspect + replay:
   ```sh
   make dlq-list
   make dlq-replay TOPIC=user.data.deletion.requested.dlq
   ```

### Case C: legal cutoff (>30 days)

If `days_old > 30` you are out of compliance. Escalate to legal + DPO immediately. Document the incident in `documentation/incidents/`. Continue remediation under Case A or B in parallel.

## Verify completion

After fixing all pending rows:

```sql
SELECT count(*) FROM user_deletions
WHERE completed_at IS NULL AND requested_at < now() - interval '25 days';
```

Should return 0. The `GDPRDeletionLag` alert auto-resolves once the metric drops to 0 (cron refreshes every 6h — or hit `/metrics` on auth to refresh now).

## Prevent recurrence

- If Case A frequent: investigate auth handler timeout / DB unavailability patterns. The 2-step (anonymise + publish) is intentionally separate — failure to publish should NOT roll back local anonymisation, but failure to anonymise must roll back the ledger.
- If Case B frequent: investigate `analytics-gdpr` consumer health. Check k8s/swarm restart history, OOM kills.
- Cron interval (currently 6h) defined in `services/auth/cmd/main.go runDeletionLagCron`. Lower if alerts are too late.
