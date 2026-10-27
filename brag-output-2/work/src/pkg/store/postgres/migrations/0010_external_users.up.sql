-- Users a host product identifies (external_id, unique per tenant when set).
ALTER TABLE ce_users ADD COLUMN external_id TEXT NULL;

CREATE UNIQUE INDEX ce_users_external_id ON ce_users (tenant_id, external_id) WHERE external_id IS NOT NULL;
