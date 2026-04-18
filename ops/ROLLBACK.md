# Rollback Runbook

## Automatic rollback

Argo Rollouts automatically rolls back if during the canary window:
- Error rate > 1%
- p99 latency increases > 50% vs baseline for 2 minutes

Monitor rollout status:
```bash
kubectl argo rollouts get rollout newsroom-<service> -n production --watch
```

## Manual rollback — Argo Rollouts abort

```bash
kubectl argo rollouts abort newsroom-<service> -n production
```

This promotes the stable version back to 100%.

## Manual rollback — Helm

List Helm revisions:
```bash
helm history newsroom -n production
```

Roll back to previous revision:
```bash
helm rollback newsroom <revision> -n production
```

## Database rollback

If a migration must be reversed:
```bash
make migrate-down ENV=staging   # test on staging first
make migrate-down ENV=production
```

**Warning:** Only run `migrate-down` if the new service version is already rolled back.
All migrations must be backward-compatible — if they are, rollback is service-only.
