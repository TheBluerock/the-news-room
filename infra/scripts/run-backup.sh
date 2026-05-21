#!/bin/sh
# Backup: pg_dump + Vault snapshot → Backblaze B2 (S3-compatible).
# Invoked by cron inside the `backup` service container (see infra/swarm/stack.prod.yml).
#
# Cron schedule (set in container's /etc/crontabs/root):
#   30 3 * * *   /usr/local/bin/run-backup.sh pg     >> /var/backups/backup.log 2>&1
#   45 3 * * *   /usr/local/bin/run-backup.sh vault  >> /var/backups/backup.log 2>&1
#   00 4 1 * *   /usr/local/bin/run-backup.sh audit  >> /var/backups/backup.log 2>&1
#
# Reads credentials from Docker secrets mounted at /run/secrets/.
# Retention: B2 lifecycle rules manage rotation (set bucket-side, not here).

set -eu

KIND="${1:-pg}"
TS=$(date -u +%Y%m%dT%H%M%SZ)
WORK=/var/backups

PGPASSWORD_FILE=/run/secrets/postgres_password
B2_KEY_ID_FILE=/run/secrets/b2_application_key_id
B2_KEY_FILE=/run/secrets/b2_application_key
VAULT_TOKEN_FILE=/run/secrets/vault_token

[ -r "$B2_KEY_ID_FILE" ] || { echo "FATAL: missing $B2_KEY_ID_FILE"; exit 1; }
[ -r "$B2_KEY_FILE" ]    || { echo "FATAL: missing $B2_KEY_FILE"; exit 1; }

export RCLONE_CONFIG_B2_TYPE=s3
export RCLONE_CONFIG_B2_PROVIDER=Other
export RCLONE_CONFIG_B2_ENDPOINT="${BACKUP_ENDPOINT:?BACKUP_ENDPOINT unset}"
RCLONE_CONFIG_B2_ACCESS_KEY_ID="$(cat "$B2_KEY_ID_FILE")"
RCLONE_CONFIG_B2_SECRET_ACCESS_KEY="$(cat "$B2_KEY_FILE")"
export RCLONE_CONFIG_B2_ACCESS_KEY_ID RCLONE_CONFIG_B2_SECRET_ACCESS_KEY
export RCLONE_CONFIG_B2_ACL=private

upload() {
  src="$1"
  remote_path="$2"
  echo "[$(date -u +%H:%M:%SZ)] uploading $src → b2:${BACKUP_BUCKET}/${remote_path}"
  rclone copyto "$src" "b2:${BACKUP_BUCKET}/${remote_path}" \
    --s3-no-check-bucket \
    --no-traverse \
    --transfers 2 \
    --retries 3
}

case "$KIND" in
  pg)
    OUT="$WORK/pg-$TS.sql.gz"
    echo "[$(date -u +%H:%M:%SZ)] starting pg_dump → $OUT"
    PGPASSWORD="$(cat "$PGPASSWORD_FILE")" pg_dump \
      --format=plain \
      --no-owner --no-privileges \
      --exclude-schema=pg_temp_* \
      | gzip -9 > "$OUT"
    upload "$OUT" "postgres/$(date -u +%Y)/$(date -u +%m)/pg-$TS.sql.gz"
    rm -f "$OUT"
    ;;

  vault)
    OUT="$WORK/vault-$TS.snap"
    echo "[$(date -u +%H:%M:%SZ)] starting vault snapshot → $OUT"
    if [ ! -r "$VAULT_TOKEN_FILE" ]; then
      echo "WARN: missing vault_token secret — skipping snapshot"
      exit 0
    fi
    curl --fail --silent \
      --header "X-Vault-Token: $(cat "$VAULT_TOKEN_FILE")" \
      --request GET \
      --output "$OUT" \
      "${VAULT_ADDR%/}/v1/sys/storage/raft/snapshot"
    upload "$OUT" "vault/$(date -u +%Y)/$(date -u +%m)/vault-$TS.snap"
    rm -f "$OUT"
    ;;

  audit)
    # Monthly archive: dump only the audit_log table; B2 bucket lifecycle keeps 1y under object lock.
    OUT="$WORK/audit-$TS.sql.gz"
    echo "[$(date -u +%H:%M:%SZ)] starting audit_log dump → $OUT"
    PGPASSWORD="$(cat "$PGPASSWORD_FILE")" pg_dump \
      --format=plain \
      --no-owner --no-privileges \
      --table=audit_log \
      | gzip -9 > "$OUT"
    upload "$OUT" "audit/$(date -u +%Y)/audit-$TS.sql.gz"
    rm -f "$OUT"
    ;;

  *)
    echo "Usage: $0 {pg|vault|audit}"
    exit 64
    ;;
esac

echo "[$(date -u +%H:%M:%SZ)] backup '$KIND' done"
