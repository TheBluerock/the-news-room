CREATE EXTENSION IF NOT EXISTS pgcrypto;

ALTER TABLE users ADD COLUMN IF NOT EXISTS password_hash TEXT NOT NULL DEFAULT '';

-- Seed dev admin — password: dev-admin-123
INSERT INTO users (email, market, password_hash)
VALUES ('admin@newsroom.dev', NULL, crypt('dev-admin-123', gen_salt('bf', 10)))
ON CONFLICT (email) DO NOTHING;

-- Seed per-market editor accounts for testing
INSERT INTO users (email, market, password_hash) VALUES
    ('editor.italy@newsroom.dev',  'italy', crypt('dev-editor-123', gen_salt('bf', 10))),
    ('editor.usa@newsroom.dev',    'usa',   crypt('dev-editor-123', gen_salt('bf', 10))),
    ('editor.china@newsroom.dev',  'china', crypt('dev-editor-123', gen_salt('bf', 10)))
ON CONFLICT (email) DO NOTHING;

-- Assign Casbin roles
INSERT INTO casbin_rule (ptype, v0, v1) VALUES
    ('g', 'admin@newsroom.dev',        'admin'),
    ('g', 'editor.italy@newsroom.dev', 'editor'),
    ('g', 'editor.usa@newsroom.dev',   'editor'),
    ('g', 'editor.china@newsroom.dev', 'editor')
ON CONFLICT DO NOTHING;
