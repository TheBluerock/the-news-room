# Rollback Runbook

## Automatic rollback (Docker Swarm)

The production stack is configured with `failure_action: rollback` and `order: start-first`.
If a new container fails its health check during a rolling update, Swarm automatically rolls
back that service to the previous image. No manual action needed.

Monitor an in-progress update:
```bash
docker service ps newsroom_<service>   # shows current and previous tasks
make swarm-status                      # all services at a glance
```

---

## Manual rollback — single service

```bash
make swarm-rollback SERVICE=<service> ENV=prod
# equivalent to:
docker service rollback newsroom_<service>
```

This reverts to the image and configuration from the previous `docker service update`.

---

## Manual rollback — full stack

If multiple services need to be rolled back simultaneously (e.g. after a bad release that
touched shared proto or migrations):

```bash
# Re-deploy the previous git SHA
export REGISTRY=ghcr.io/<org>
export TAG=<previous-sha>
export VAULT_ADDR=https://vault.example.com

make swarm-deploy ENV=prod STACK=newsroom
```

Swarm will rolling-update each service to the previous image in parallel.

---

## Database rollback

Only run if the code rollback alone is not sufficient (i.e. the migration introduced a
breaking schema change — which should never happen by policy, but in emergencies):

```bash
# Always roll back service first, THEN migrate down
make swarm-rollback SERVICE=<service> ENV=prod
make migrate-down ENV=prod
```

**Warning:** `migrate-down` is destructive if the migration added columns with data.
Test on staging first. All migrations must be backward-compatible — if they are, service
rollback alone is sufficient without touching the DB.

---

## Verify after rollback

```bash
make swarm-status
docker service logs newsroom_<service> --tail 50
curl https://newsroom.example/health
```

Check Grafana for error rate and latency recovery.
