DELETE FROM casbin_rule WHERE ptype = 'g' AND v1 IN ('admin', 'editor');
DELETE FROM users WHERE email IN (
    'admin@newsroom.dev',
    'editor.italy@newsroom.dev',
    'editor.usa@newsroom.dev',
    'editor.china@newsroom.dev'
);
ALTER TABLE users DROP COLUMN IF EXISTS password_hash;
