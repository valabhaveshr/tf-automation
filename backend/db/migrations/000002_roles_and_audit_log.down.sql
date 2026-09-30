-- WS-A: Data Layer — revert Phase 3 roles + audit log
-- Drops in dependency-safe order (reverse of creation / FK dependents first).

DROP TABLE IF EXISTS audit_log;
DROP TABLE IF EXISTS user_roles;
DROP TABLE IF EXISTS roles;

ALTER TABLE users DROP COLUMN IF EXISTS auth_provider;
ALTER TABLE users DROP COLUMN IF EXISTS sso_subject;
