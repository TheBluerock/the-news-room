#!/usr/bin/env bash
# Seeds local Vault dev mode with dummy credentials.
# Production credentials are managed via Vault UI / Terraform.
set -euo pipefail

export VAULT_ADDR=${VAULT_ADDR:-http://localhost:8200}
export VAULT_TOKEN=${VAULT_TOKEN:-dev-root-token}

echo "==> Enabling KV secrets engine..."
vault secrets enable -path=secret kv-v2 2>/dev/null || true

echo "==> Writing service credentials..."

vault kv put secret/newsroom/auth \
  jwt_private_key="$(openssl genrsa 2048 2>/dev/null)" \
  postgres_user="newsroom" \
  postgres_password="newsroom_dev" \
  postgres_dsn="postgres://newsroom:newsroom_dev@postgres:5432/newsroom?sslmode=disable" \
  redis_addr="redis:6379"

vault kv put secret/newsroom/learner \
  postgres_dsn="postgres://newsroom:newsroom_dev@postgres:5432/newsroom?sslmode=disable" \
  openai_api_key="sk-dev-placeholder" \
  redpanda_brokers="redpanda:29092"

vault kv put secret/newsroom/agent \
  openai_api_key="sk-dev-placeholder" \
  anthropic_api_key="sk-ant-dev-placeholder" \
  redis_addr="redis:6379" \
  redpanda_brokers="redpanda:29092"

vault kv put secret/newsroom/moderation \
  openai_api_key="sk-dev-placeholder" \
  anthropic_api_key="sk-ant-dev-placeholder"

vault kv put secret/newsroom/correction \
  redis_addr="redis:6379" \
  redpanda_brokers="redpanda:29092"

vault kv put secret/newsroom/analytics \
  postgres_dsn="postgres://newsroom:newsroom_dev@postgres:5432/newsroom?sslmode=disable" \
  redpanda_brokers="redpanda:29092"

echo "==> Writing service policies..."
for svc in auth learner agent moderation correction analytics; do
  vault policy write "newsroom-${svc}" "infra/vault/policies/${svc}.hcl"
done

echo "==> Vault seeded successfully."
