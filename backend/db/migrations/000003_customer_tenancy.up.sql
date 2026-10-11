-- 000003: multi-tenant customers (invite-only onboarding).
--
-- Every customer-side user belongs to exactly one customer and only sees that
-- customer's resources (records). Platform admins are users holding the
-- 'admin' role with no customer (users.customer_id IS NULL); they invite a
-- customer's first admin, who completes the customer profile on first login
-- (onboarding) and can then invite their own team.

-- Customer-scoped admin: completes onboarding, edits the customer profile,
-- invites users into their own customer. Distinct from the global 'admin'
-- (platform) role.
INSERT INTO roles (name) VALUES ('customer_admin')
    ON CONFLICT (name) DO NOTHING;

-- Customer profile captured at onboarding. name stays the org name (and the
-- first path segment of the output repo layout); business_short_code is the
-- unique short name, e.g. "dmsc" for Doha Marine Sports Centre, also used by
-- the naming convention engine.
ALTER TABLE customers ADD COLUMN contact_name   TEXT NOT NULL DEFAULT '';
ALTER TABLE customers ADD COLUMN contact_email  TEXT NOT NULL DEFAULT '';
ALTER TABLE customers ADD COLUMN contact_phone  TEXT NOT NULL DEFAULT '';
ALTER TABLE customers ADD COLUMN country        TEXT NOT NULL DEFAULT '';
ALTER TABLE customers ADD COLUMN industry       TEXT NOT NULL DEFAULT '';
ALTER TABLE customers ADD COLUMN default_cloud  TEXT NOT NULL DEFAULT 'gcp';
ALTER TABLE customers ADD COLUMN default_region TEXT NOT NULL DEFAULT '';
ALTER TABLE customers ADD COLUMN gcp_organization_id TEXT NOT NULL DEFAULT '';
ALTER TABLE customers ADD COLUMN billing_account_id  TEXT NOT NULL DEFAULT '';

-- Duplicate customer short names are not allowed. Case-insensitive, though the
-- application already stores them lowercase.
CREATE UNIQUE INDEX customers_business_short_code_key ON customers (lower(business_short_code));

-- Tenancy key for users. NULL = platform-side user (platform admin), or a
-- customer admin who hasn't completed onboarding yet.
ALTER TABLE users ADD COLUMN customer_id UUID REFERENCES customers(id);
ALTER TABLE users ADD COLUMN email TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_users_customer_id ON users (customer_id);

CREATE INDEX idx_records_customer_id ON records (customer_id);

-- Invitations replace self-signup. Only a SHA-256 hash of the token is stored;
-- the raw token is shown once to the inviter as a link (/invite/<token>).
CREATE TABLE invitations (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash   TEXT NOT NULL UNIQUE,
    email        TEXT NOT NULL,
    role         TEXT NOT NULL,                     -- customer_admin | requester
    customer_id  UUID REFERENCES customers(id),     -- NULL = new customer, onboarding on first login
    invited_by   TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at   TIMESTAMPTZ NOT NULL,
    accepted_at  TIMESTAMPTZ,
    accepted_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    revoked_at   TIMESTAMPTZ
);
CREATE INDEX idx_invitations_customer_id ON invitations (customer_id);
