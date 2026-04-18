# Disaster Recovery Runbook

## RPO / RTO targets

| Component   | RPO        | RTO       | Backup method                            |
|-------------|------------|-----------|------------------------------------------|
| PostgreSQL  | 15 minutes | < 1 hour  | WAL → S3 (pgBackRest) + daily pg_dump   |
| Redis       | 24 hours   | < 30 min  | Daily RDB snapshot → S3 + AOF            |
| Vault       | 24 hours   | < 30 min  | Daily Raft snapshot → S3                 |
| RedPanda    | 7 days     | < 15 min  | RF=3 + S3 tiered storage                 |

## PostgreSQL restore

```bash
# 1. Stop all services writing to PostgreSQL
# 2. Restore from S3 via pgBackRest:
pgbackrest --stanza=newsroom restore
# 3. Or restore from daily dump:
aws s3 cp s3://<bucket>/postgres/newsroom-<date>.dump /tmp/
pg_restore -d newsroom /tmp/newsroom-<date>.dump
# 4. Run make migrate-up ENV=production to ensure schema is current
# 5. Restart services
```

## Redis restore

```bash
# 1. Stop services reading/writing Redis
# 2. Download RDB snapshot:
aws s3 cp s3://<bucket>/redis/dump-<date>.rdb /var/lib/redis/dump.rdb
# 3. Restart Redis — it loads RDB on startup
# 4. Restart services
```

## Vault restore

See ops/VAULT-DR.md.

## Restore drill schedule

Mandatory quarterly. Log results in this file:

| Date | Component | Outcome | Time to restore | Notes |
|------|-----------|---------|-----------------|-------|
| TBD  | PostgreSQL | —      | —               | Schedule before go-live |
