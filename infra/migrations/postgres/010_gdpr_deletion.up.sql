-- Phase K2: GDPR worldwide deletion ledger.
--
-- Owner decision 2026-05-21: 30-day uniform deletion right across markets.
-- The ledger tracks each user.data.deletion.requested event and which
-- services have ack'd completion. A cron in auth alerts at day 25.
--
-- We target the `public.users` table that auth's runtime code actually queries
-- (see services/auth/internal/store/db.go). Schema-per-service consolidation
-- is Phase N5; until then we keep PII work where the live code already lives.

-- Soft-delete column needed for anonymisation stamping. Migration 005 added
-- this on auth_svc.users but the live code path uses public.users — add it here.
ALTER TABLE users ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS user_deletions (
    user_id             UUID PRIMARY KEY,
    requested_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    requested_by        UUID NOT NULL,
    completed_at        TIMESTAMPTZ,
    -- Shape: {"auth": "2026-05-21T12:00:00Z", "analytics": "...", ...}
    services_completed  JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS user_deletions_pending_idx
    ON user_deletions (requested_at)
    WHERE completed_at IS NULL;

-- Sentinel user — referenced by anonymised rows so audit_log + editorial_calendar
-- preserve FK integrity. ID is well-known so the application can repoint at it
-- without a lookup.
INSERT INTO users (id, email, password_hash)
VALUES (
    '00000000-0000-0000-0000-000000000001',
    'anonymised@deleted.local',
    ''
)
ON CONFLICT (id) DO NOTHING;
