#!/usr/bin/env bash
# Creates Redpanda topics, DLQ topics, and registers JSON schemas in Schema Registry.
set -euo pipefail

BROKER="${REDPANDA_BROKER:-localhost:9092}"
SCHEMA_REGISTRY="${SCHEMA_REGISTRY_URL:-http://localhost:8081}"
SCHEMAS_DIR="$(cd "$(dirname "$0")/../../infra/schemas" && pwd)"

# ── Topic definitions ──────────────────────────────────────────────────────────
declare -A TOPIC_PARTITIONS=(
  ["topic.trending"]=3
  ["article.generated"]=3
  ["article.approved"]=3
  ["article.published"]=3
  ["editor.correction"]=3
  ["moderation.rejected"]=3
  ["graph.updated"]=3
  ["user.data.deletion.requested"]=1
)

create_topic() {
  local name="$1"
  local partitions="$2"
  if rpk topic create "$name" \
      --partitions "$partitions" \
      --replicas 1 \
      --brokers "$BROKER" 2>&1 | grep -qE 'TOPIC_ALREADY_EXISTS|Created'; then
    echo "  topic: $name (partitions=$partitions)"
  fi
  # DLQ topic
  local dlq="${name}.dlq"
  rpk topic create "$dlq" \
    --partitions 1 \
    --replicas 1 \
    --brokers "$BROKER" > /dev/null 2>&1 || true
  echo "  topic: $dlq (dlq)"
}

echo "==> Creating topics..."
for topic in "${!TOPIC_PARTITIONS[@]}"; do
  create_topic "$topic" "${TOPIC_PARTITIONS[$topic]}"
done

# ── Schema registration ────────────────────────────────────────────────────────
register_schema() {
  local subject="$1"
  local schema_file="$2"
  if [[ ! -f "$schema_file" ]]; then
    echo "  WARN: schema file not found: $schema_file"
    return
  fi
  local schema
  schema=$(python3 -c "import json,sys; print(json.dumps(json.load(open('$schema_file'))))")
  local payload
  payload=$(python3 -c "import json; print(json.dumps({'schema': json.dumps(json.loads(open('$schema_file').read())), 'schemaType': 'JSON'}))")
  local status
  status=$(curl -sf -o /dev/null -w "%{http_code}" -X POST \
    -H "Content-Type: application/vnd.schemaregistry.v1+json" \
    -d "$payload" \
    "$SCHEMA_REGISTRY/subjects/${subject}-value/versions" 2>/dev/null || echo "000")
  if [[ "$status" =~ ^2 ]]; then
    echo "  schema: ${subject}-value"
  else
    echo "  WARN: schema registration failed for $subject (HTTP $status)"
  fi
}

echo "==> Registering schemas..."
register_schema "topic.trending"                 "$SCHEMAS_DIR/topic.trending.json"
register_schema "article.generated"              "$SCHEMAS_DIR/article.generated.json"
register_schema "article.approved"               "$SCHEMAS_DIR/article.approved.json"
register_schema "article.published"              "$SCHEMAS_DIR/article.published.json"
register_schema "editor.correction"              "$SCHEMAS_DIR/editor.correction.json"
register_schema "moderation.rejected"            "$SCHEMAS_DIR/moderation.rejected.json"
register_schema "graph.updated"                  "$SCHEMAS_DIR/graph.updated.json"
register_schema "user.data.deletion.requested"   "$SCHEMAS_DIR/user.data.deletion.requested.json"

echo "==> Redpanda setup complete."
