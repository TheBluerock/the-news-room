-- Reverse Phase K2 GDPR ledger.
-- Sentinel user row is NOT removed: audit_log rows may already reference it
-- and removing it would break FKs or require destructive updates. Leaving the
-- sentinel is safe (no real identity) and avoids data loss.
-- deleted_at column is NOT removed either — keeps existing soft-deleted rows
-- meaningful if up.sql is re-applied later.

DROP INDEX IF EXISTS user_deletions_pending_idx;
DROP TABLE IF EXISTS user_deletions;
