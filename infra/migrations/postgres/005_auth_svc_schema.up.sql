-- auth_svc schema: users, JWT key pairs, Casbin RBAC rules

CREATE SCHEMA IF NOT EXISTS auth_svc;

-- JWT RS256 key pairs — supports key rotation with 15-minute overlap window
CREATE TABLE IF NOT EXISTS auth_svc.jwt_keys (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    public_key  TEXT NOT NULL,
    private_key TEXT NOT NULL,
    active      BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS jwt_keys_active_idx ON auth_svc.jwt_keys (active) WHERE active = true;

-- Users (soft delete for GDPR — DELETE /api/user/data sets deleted_at)
CREATE TABLE IF NOT EXISTS auth_svc.users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email         TEXT UNIQUE NOT NULL,
    role          TEXT NOT NULL DEFAULT 'viewer',
    market        TEXT CHECK (market IN ('italy', 'usa', 'china')),
    password_hash TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at    TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS users_email_idx ON auth_svc.users (email) WHERE deleted_at IS NULL;

-- Casbin RBAC rules (native PostgreSQL adapter format)
CREATE TABLE IF NOT EXISTS auth_svc.casbin_rule (
    id    BIGSERIAL PRIMARY KEY,
    ptype TEXT NOT NULL,
    v0    TEXT, v1 TEXT, v2 TEXT,
    v3    TEXT, v4 TEXT, v5 TEXT
);

-- Copy users from public schema; derive role from g-type casbin entries
INSERT INTO auth_svc.users (id, email, role, market, password_hash, created_at)
SELECT DISTINCT ON (u.id)
    u.id,
    u.email,
    COALESCE(cr.v1, 'viewer') AS role,
    u.market,
    COALESCE(u.password_hash, ''),
    u.created_at
FROM users u
LEFT JOIN casbin_rule cr ON cr.ptype = 'g' AND cr.v0 = u.email
ORDER BY u.id
ON CONFLICT (email) DO NOTHING;

-- Copy casbin rules from public schema
INSERT INTO auth_svc.casbin_rule (ptype, v0, v1, v2, v3, v4, v5)
SELECT ptype, v0, v1, v2, v3, v4, v5 FROM casbin_rule
ON CONFLICT DO NOTHING;

-- Per-service role with minimal privileges
DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'auth_rw') THEN
        CREATE ROLE auth_rw;
    END IF;
END $$;

GRANT USAGE ON SCHEMA auth_svc TO auth_rw;
GRANT SELECT, INSERT, UPDATE ON ALL TABLES IN SCHEMA auth_svc TO auth_rw;
GRANT USAGE ON SEQUENCE auth_svc.casbin_rule_id_seq TO auth_rw;
ALTER DEFAULT PRIVILEGES IN SCHEMA auth_svc GRANT SELECT, INSERT, UPDATE ON TABLES TO auth_rw;
ALTER DEFAULT PRIVILEGES IN SCHEMA auth_svc GRANT USAGE ON SEQUENCES TO auth_rw;
