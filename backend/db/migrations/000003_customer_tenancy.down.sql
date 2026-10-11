DROP TABLE IF EXISTS invitations;

DROP INDEX IF EXISTS idx_records_customer_id;

DROP INDEX IF EXISTS idx_users_customer_id;
ALTER TABLE users DROP COLUMN IF EXISTS email;
ALTER TABLE users DROP COLUMN IF EXISTS customer_id;

DROP INDEX IF EXISTS customers_business_short_code_key;

ALTER TABLE customers DROP COLUMN IF EXISTS billing_account_id;
ALTER TABLE customers DROP COLUMN IF EXISTS gcp_organization_id;
ALTER TABLE customers DROP COLUMN IF EXISTS default_region;
ALTER TABLE customers DROP COLUMN IF EXISTS default_cloud;
ALTER TABLE customers DROP COLUMN IF EXISTS industry;
ALTER TABLE customers DROP COLUMN IF EXISTS country;
ALTER TABLE customers DROP COLUMN IF EXISTS contact_phone;
ALTER TABLE customers DROP COLUMN IF EXISTS contact_email;
ALTER TABLE customers DROP COLUMN IF EXISTS contact_name;

DELETE FROM roles WHERE name = 'customer_admin';
