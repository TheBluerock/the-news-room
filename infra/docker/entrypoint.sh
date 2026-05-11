#!/bin/sh
# Vault AppRole login — fetches a short-lived token before exec'ing the service.
# Expects Docker Swarm secrets mounted at /run/secrets/vault_role_id and vault_secret_id.
# Sets VAULT_TOKEN in the process environment; services call Vault directly.
set -eu

VAULT_ADDR="${VAULT_ADDR:-http://vault:8200}"
ROLE_ID_FILE="${VAULT_ROLE_ID_FILE:-/run/secrets/vault_role_id}"
SECRET_ID_FILE="${VAULT_SECRET_ID_FILE:-/run/secrets/vault_secret_id}"

if [ ! -f "$ROLE_ID_FILE" ] || [ ! -f "$SECRET_ID_FILE" ]; then
  echo "[entrypoint] ERROR: Vault AppRole credentials not found at $ROLE_ID_FILE / $SECRET_ID_FILE" >&2
  exit 1
fi

ROLE_ID=$(cat "$ROLE_ID_FILE")
SECRET_ID=$(cat "$SECRET_ID_FILE")

LOGIN_RESPONSE=$(wget -qO- \
  --header="Content-Type: application/json" \
  --post-data="{\"role_id\":\"$ROLE_ID\",\"secret_id\":\"$SECRET_ID\"}" \
  "$VAULT_ADDR/v1/auth/approle/login") || {
  echo "[entrypoint] ERROR: Vault AppRole login failed" >&2
  exit 1
}

# Extract client_token using only shell builtins + tr/sed (no jq dependency)
VAULT_TOKEN=$(printf '%s' "$LOGIN_RESPONSE" | tr ',' '\n' | grep '"client_token"' | sed 's/.*"client_token":"\([^"]*\)".*/\1/')

if [ -z "$VAULT_TOKEN" ]; then
  echo "[entrypoint] ERROR: could not extract client_token from Vault response" >&2
  exit 1
fi

export VAULT_TOKEN
echo "[entrypoint] Vault token acquired, starting service"
exec "$@"
