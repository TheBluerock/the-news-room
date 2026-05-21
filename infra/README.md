# Infrastructure

Deploy target: **Docker Swarm on Contabo VPS**. See [memory: project-deploy-target](../documentation/10-implementation-plan.md#phase-c--self-hosted-bootstrap-35-days) for context.

```
infra/
├── ansible/        Bootstrap playbook (provision Docker + Swarm on fresh VPS)
├── swarm/          docker stack files (stack.dev.yml, stack.prod.yml)
├── caddy/          Reverse proxy + auto-TLS config
├── docker/         Shared scripts baked into images (vault-agent entrypoint)
├── grafana/        Dashboards (TODO Phase E)
├── helm/           DEPRECATED — see helm/DEPRECATED.md
├── migrations/     PostgreSQL migrations (golang-migrate format)
├── prometheus/     Prometheus scrape config + recording rules
├── schemas/        RedPanda Schema Registry definitions (event contracts)
├── scripts/        Operational scripts (backup, redpanda setup)
├── swarm/          Stack files
├── tempo/          OTel trace backend config
└── vault/          Vault policies + Agent sidecar templates
```

## End-to-end deploy (fresh Contabo VPS S — staging)

### 0. Prerequisites on your laptop

```sh
pip3 install --user ansible-core ansible-lint
ansible-galaxy collection install -r infra/ansible/requirements.yml
```

### 1. Provision VPS

- Order Contabo VPS S (€5.50/mo, 4 vCPU / 8GB / 200GB NVMe), Debian 12.
- Set root password during checkout.
- Add your SSH public key during checkout (or via Contabo control panel).

### 2. Bootstrap with Ansible

```sh
cd infra/ansible
cp inventory.example.ini inventory.ini
cp group_vars/all.yml.example group_vars/all.yml
$EDITOR inventory.ini             # paste VPS IP under [swarm_managers]
$EDITOR group_vars/all.yml        # tune admin_user, B2 endpoint, swarm subnet

ansible-playbook bootstrap.yml --ask-become-pass
```

This installs Docker, hardens UFW, applies sysctl tuning, initialises Swarm, labels the node `role=infra`, and starts `node_exporter` + `cAdvisor` containers.

Validate:
```sh
ssh root@<vps-ip> 'docker node ls'
```

### 3. Create Docker secrets

On the manager node:

```sh
# Cluster-wide passwords
printf 'CHANGEME' | docker secret create postgres_password -
printf 'CHANGEME' | docker secret create grafana_admin_password -

# Backblaze B2 (S3-compatible) backup credentials
printf 'KEY_ID' | docker secret create b2_application_key_id -
printf 'APP_KEY' | docker secret create b2_application_key -

# Vault AppRole creds per service — generated on first Vault unseal
# See ops/VAULT-DR.md for the full bootstrap procedure.
for svc in auth learner agent moderation analytics sanity; do
  printf 'ROLE_ID'   | docker secret create "vault_role_id_${svc}" -
  printf 'SECRET_ID' | docker secret create "vault_secret_id_${svc}" -
done
printf 'VAULT_TOKEN' | docker secret create vault_token -
```

### 4. Push images to registry

```sh
export REGISTRY=ghcr.io/<your-org>/newsroom
export TAG=$(git rev-parse --short HEAD)

for svc in auth learner agent moderation analytics sanity; do
  docker build -t "$REGISTRY/$svc:$TAG" services/$svc
  docker push "$REGISTRY/$svc:$TAG"
done
```

### 5. Deploy stack

```sh
export VAULT_ADDR=https://vault.newsroom.example
export BACKUP_BUCKET=newsroom-backups
export BACKUP_ENDPOINT=https://s3.eu-central-003.backblazeb2.com

docker stack deploy \
  -c infra/swarm/stack.prod.yml \
  --with-registry-auth \
  newsroom
```

Watch convergence:
```sh
watch docker service ls
```

### 6. Apply migrations

```sh
docker run --rm \
  --network newsroom_newsroom_net \
  -v "$PWD/infra/migrations/postgres":/migrations \
  migrate/migrate:v4 \
  -path=/migrations \
  -database "postgres://newsroom:$(cat /run/secrets/postgres_password)@postgres:5432/newsroom?sslmode=disable" \
  up
```

### 7. Smoke test

```sh
curl -sf https://newsroom.example/api/auth/health    # → 200 {"status":"ok"}
curl -sf https://newsroom.example/api/learner/health
```

End-to-end: trigger a topic.trending event, watch it land in Sanity:
```sh
ops/smoke-test-pipeline.sh   # TODO Phase E
```

## Production scaling (multi-node)

After staging is stable, add prod workers (VPS L):

1. Order 2× Contabo VPS L (~€15/mo each).
2. Add to `inventory.ini` under `[swarm_workers]` with `role_label=app`.
3. Re-run `ansible-playbook bootstrap.yml` — playbook is idempotent.
4. New nodes auto-join; placement constraints route `role=infra` services to the manager and `role=app` to workers.

## Backups

The `backup` service in `stack.prod.yml` runs cron:

- **03:30 UTC daily** — `pg_dump` → `b2://${BACKUP_BUCKET}/postgres/YYYY/MM/`
- **03:45 UTC daily** — Vault Raft snapshot → `b2://${BACKUP_BUCKET}/vault/YYYY/MM/`
- **04:00 UTC monthly (1st)** — `audit_log` table dump → `b2://${BACKUP_BUCKET}/audit/YYYY/` (under B2 object lock, 1y retention).

Verify recent backups:
```sh
docker service logs newsroom_backup --tail 50
```

Restore drill (quarterly):
```sh
ops/restore-test.sh  # TODO Phase F
```

## Switching deploy targets

If you ever need to migrate off Contabo:

- **→ AWS EC2 + EBS**: same Ansible playbook (Debian/Ubuntu agnostic). Replace Backblaze with AWS S3.
- **→ Kubernetes**: revive `infra/helm/` per `infra/helm/DEPRECATED.md` revival steps.

Decision log: `documentation/10-implementation-plan.md` Phase C.
