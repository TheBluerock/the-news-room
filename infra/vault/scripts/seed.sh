#!/usr/bin/env bash
# Seeds local Vault dev mode with dummy credentials via HTTP API (no vault CLI needed).
set -euo pipefail

VAULT_ADDR="${VAULT_ADDR:-http://localhost:8200}"
VAULT_TOKEN="${VAULT_TOKEN:-dev-root-token}"

kv_put() {
  local path="$1"; shift
  local data="{}"
  for pair in "$@"; do
    key="${pair%%=*}"
    val="${pair#*=}"
    data=$(printf '%s' "$data" | python3 -c "
import sys, json
d = json.load(sys.stdin)
d['$key'] = '$val'
print(json.dumps(d))")
  done
  curl -sf -X POST \
    -H "X-Vault-Token: $VAULT_TOKEN" \
    -H "Content-Type: application/json" \
    -d "{\"data\": $data}" \
    "$VAULT_ADDR/v1/secret/data/$path" > /dev/null
  echo "  wrote $path"
}

echo "==> Enabling KV v2 secrets engine..."
curl -sf -X POST \
  -H "X-Vault-Token: $VAULT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"type":"kv","options":{"version":"2"}}' \
  "$VAULT_ADDR/v1/sys/mounts/secret" > /dev/null 2>&1 || true

echo "==> Writing service credentials..."

JWT_KEY=$(openssl genrsa 2048 2>/dev/null | tr '\n' '§' | sed 's/§/\\n/g')

kv_put newsroom/auth \
  "jwt_private_key=$JWT_KEY" \
  "postgres_dsn=postgres://newsroom:newsroom_dev@postgres:5432/newsroom?sslmode=disable" \
  "redis_addr=redis:6379"

kv_put newsroom/learner \
  "postgres_dsn=postgres://newsroom:newsroom_dev@postgres:5432/newsroom?sslmode=disable" \
  "redis_addr=redis:6379" \
  "openai_api_key=sk-dev-placeholder" \
  "redpanda_brokers=redpanda:29092"

kv_put newsroom/agent \
  "openai_api_key=sk-dev-placeholder" \
  "anthropic_api_key=sk-ant-dev-placeholder" \
  "redis_addr=redis:6379" \
  "redpanda_brokers=redpanda:29092"

kv_put newsroom/moderation \
  "openai_api_key=sk-dev-placeholder" \
  "anthropic_api_key=sk-ant-dev-placeholder" \
  "redpanda_brokers=redpanda:29092"

kv_put newsroom/correction \
  "redis_addr=redis:6379" \
  "redpanda_brokers=redpanda:29092"

kv_put newsroom/analytics \
  "postgres_dsn=postgres://newsroom:newsroom_dev@postgres:5432/newsroom?sslmode=disable" \
  "redis_addr=redis:6379" \
  "redpanda_brokers=redpanda:29092"

echo "==> Vault seeded successfully."
