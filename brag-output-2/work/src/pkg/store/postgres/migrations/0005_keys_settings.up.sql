-- Wrapped per-tenant data keys (deleting a row crypto-shreds the tenant) and
-- per-workspace settings, both as JSON documents with an optimistic version.
CREATE TABLE ce_tenant_keys (
  tenant_id TEXT   PRIMARY KEY,
  doc       JSONB  NOT NULL,
  version   BIGINT NOT NULL
);

CREATE TABLE ce_settings (
  tenant_id    TEXT   NOT NULL,
  workspace_id TEXT   NOT NULL,
  doc          JSONB  NOT NULL,
  version      BIGINT NOT NULL,
  PRIMARY KEY (tenant_id, workspace_id)
);
