# Vault Disaster Recovery

## Restore Vault from Raft snapshot (S3)

```bash
# 1. Start a new Vault node
# 2. Initialize it:
vault operator init -key-shares=5 -key-threshold=3

# 3. Download latest Raft snapshot from S3:
aws s3 cp s3://<bucket>/vault/vault-raft-<date>.snap /tmp/vault.snap

# 4. Restore:
vault operator raft snapshot restore /tmp/vault.snap

# 5. Unseal with 3 of 5 key shares (stored in separate secure locations)
vault operator unseal <key-1>
vault operator unseal <key-2>
vault operator unseal <key-3>
```

## Unseal key storage

Unseal keys are split across 5 holders. 3 are required to unseal.
Key locations: [FILL IN — e.g. 1Password vaults for each key holder]

## JWT RS256 key rotation

JWT rotation uses a 15-minute overlap window:
1. Generate new keypair, write to Vault under new version
2. Deploy auth service — it serves both old and new public keys during overlap
3. After 15 minutes, revoke old key version

```bash
# Rotate JWT key:
vault kv put secret/newsroom/auth jwt_private_key="$(openssl genrsa 2048 2>/dev/null)"
# Trigger auth service rolling restart (reads new key from sidecar):
kubectl rollout restart deployment/newsroom-auth -n production
```

## PostgreSQL password rotation

Vault rotates PostgreSQL passwords automatically every 30 days.
Services reload connection pool from `/vault/secrets/postgres_dsn` via the Vault Agent sidecar.
No service restart required.
