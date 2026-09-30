-- WS-A: Data Layer — initial schema
-- See plan/ARCHITECTURE.md § Phase 1 → Data model for the authoritative source of this schema.

CREATE EXTENSION IF NOT EXISTS pgcrypto; -- provides gen_random_uuid()

CREATE TABLE module_schemas (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    module_name   TEXT NOT NULL,
    version_tag   TEXT NOT NULL,       -- e.g. "vm-instance/v1.2.0"
    metadata      JSONB NOT NULL,
    variables     JSONB NOT NULL,      -- normalized VarSchema[]
    parsed_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    is_latest     BOOLEAN NOT NULL DEFAULT true,
    UNIQUE (module_name, version_tag)
);

CREATE TABLE customers (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                TEXT NOT NULL UNIQUE,
    business_short_code TEXT NOT NULL,    -- <=5 chars, validated format, admin-editable
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE records (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),   -- globally unique ID
    customer_name   TEXT NOT NULL,
    customer_id     UUID REFERENCES customers(id),
    project_name    TEXT NOT NULL,
    env_name        TEXT NOT NULL,
    output_path     TEXT NOT NULL,     -- slugified customer/project/env
    is_active       BOOLEAN NOT NULL DEFAULT true,   -- soft-delete flag
    created_by      TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by      TEXT NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_commit_sha TEXT,
    sync_status     TEXT NOT NULL DEFAULT 'pending',   -- pending | synced | failed
    UNIQUE (customer_name, project_name, env_name)
);

CREATE TABLE module_selections (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    record_id         UUID NOT NULL REFERENCES records(id) ON DELETE CASCADE,
    module_name       TEXT NOT NULL,
    pinned_version    TEXT NOT NULL,     -- git tag pinned at submission time, e.g. "vm-instance/v1.2.0"
    module_schema_id  UUID NOT NULL REFERENCES module_schemas(id),
    instances         JSONB NOT NULL,    -- { "web-01": {...}, "web-02": {...} } -> becomes the for_each map
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (record_id, module_name)
);

CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    is_admin      BOOLEAN NOT NULL DEFAULT false,   -- lightweight Phase 1 gate; superseded by Phase 3's role model
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE naming_templates (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    cloud         TEXT NOT NULL,          -- gcp | aws | azure
    resource_type TEXT NOT NULL,          -- e.g. gcp_project, gcp_folder, gcp_vpc
    template      TEXT NOT NULL,          -- e.g. "prj-{business_short_code}-{env}-{user_label}-{num_id}"
    num_id_width  INT NOT NULL DEFAULT 2,
    created_by    TEXT NOT NULL,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (cloud, resource_type)
);

CREATE TABLE naming_counters (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_short_code  TEXT NOT NULL,
    env_name             TEXT NOT NULL,
    resource_type        TEXT NOT NULL,
    next_value           INT NOT NULL DEFAULT 1,
    UNIQUE (business_short_code, env_name, resource_type)
);
