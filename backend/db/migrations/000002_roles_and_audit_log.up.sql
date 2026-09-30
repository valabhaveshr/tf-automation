-- WS-A: Data Layer — Phase 3: roles + audit log
-- See plan/ARCHITECTURE.md § Phase 3 for the role model and audit log design.
--
-- Role model is GLOBAL per user (not scoped per customer/project): Phase 1's
-- admin-gated actions (catalog refresh, naming-template management) are
-- org-wide settings, so per-customer scoping would be premature. Three fixed
-- roles: "requester" (baseline, every authenticated user has it), "admin"
-- (superset of Phase 1's users.is_admin), "approver" (reserved/unused until
-- Phase 2 resumes).
--
-- users.is_admin is kept for backward read compatibility; role membership
-- (user_roles) becomes the source of truth going forward.

CREATE TABLE roles (
    id   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT UNIQUE NOT NULL
);

INSERT INTO roles (name) VALUES ('requester'), ('admin'), ('approver')
    ON CONFLICT (name) DO NOTHING;

CREATE TABLE user_roles (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, role_id)
);

-- OIDC 'sub' claim; null for Phase-1-era built-in (local) accounts.
ALTER TABLE users ADD COLUMN sso_subject TEXT UNIQUE;
-- 'local' | 'oidc'.
ALTER TABLE users ADD COLUMN auth_provider TEXT NOT NULL DEFAULT 'local';

-- Backfill: the is_admin -> role migration called for by plan/ARCHITECTURE.md
-- Phase 3. Every existing user gets 'requester'; every user with
-- is_admin = true additionally gets 'admin'. Idempotent (safe to re-run).
INSERT INTO user_roles (user_id, role_id)
SELECT u.id, r.id
FROM users u, roles r
WHERE r.name = 'requester'
ON CONFLICT DO NOTHING;

INSERT INTO user_roles (user_id, role_id)
SELECT u.id, r.id
FROM users u, roles r
WHERE r.name = 'admin' AND u.is_admin = true
ON CONFLICT DO NOTHING;

CREATE TABLE audit_log (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Nullable, ON DELETE SET NULL: the audit row must survive the actor
    -- user being deleted later; actor_username is the durable record.
    actor_user_id  UUID REFERENCES users(id) ON DELETE SET NULL,
    actor_username TEXT NOT NULL,
    action         TEXT NOT NULL,          -- e.g. "record.create"
    resource_type  TEXT NOT NULL,          -- record | catalog | naming_template | customer
    resource_id    TEXT,
    detail         JSONB,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- GET /admin/audit-log (contracts/openapi.yaml) filters/paginates by all
-- three of these.
CREATE INDEX idx_audit_log_resource ON audit_log (resource_type, resource_id);
CREATE INDEX idx_audit_log_actor_username ON audit_log (actor_username);
CREATE INDEX idx_audit_log_created_at ON audit_log (created_at);
