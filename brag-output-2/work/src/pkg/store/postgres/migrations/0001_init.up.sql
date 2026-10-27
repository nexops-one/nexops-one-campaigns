CREATE TABLE ce_revisions (
  tenant_id    TEXT NOT NULL,
  workspace_id TEXT NOT NULL,
  number       BIGINT NOT NULL,
  ingestion_id TEXT NOT NULL,
  kind         TEXT NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (tenant_id, workspace_id, number)
);

-- One row per record version. A version is visible in revisions
-- valid_from <= n < valid_to (valid_to NULL = still current).
CREATE TABLE ce_record_versions (
  tenant_id              TEXT NOT NULL,
  workspace_id           TEXT NOT NULL,
  entity                 TEXT NOT NULL,
  record_key             TEXT NOT NULL,
  valid_from             BIGINT NOT NULL,
  valid_to               BIGINT NULL,
  data                   JSONB NOT NULL,
  hash                   TEXT NOT NULL,
  schema_version         TEXT NOT NULL,
  source_system          TEXT NOT NULL,
  source_adapter         TEXT NOT NULL,
  source_adapter_version TEXT NOT NULL,
  ingestion_id           TEXT NOT NULL,
  source_record_ref      TEXT NOT NULL DEFAULT '',
  written_at             TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (tenant_id, workspace_id, entity, record_key, valid_from)
);
CREATE INDEX ce_record_versions_current ON ce_record_versions (tenant_id, workspace_id, entity, record_key) WHERE valid_to IS NULL;
CREATE INDEX ce_record_versions_range ON ce_record_versions (tenant_id, workspace_id, valid_from, valid_to);

CREATE TABLE ce_ingestions (
  tenant_id              TEXT NOT NULL,
  workspace_id           TEXT NOT NULL,
  id                     TEXT NOT NULL,
  batch_id               TEXT NOT NULL DEFAULT '',
  source_system          TEXT NOT NULL DEFAULT '',
  source_adapter         TEXT NOT NULL DEFAULT '',
  source_adapter_version TEXT NOT NULL DEFAULT '',
  mode                   TEXT NOT NULL DEFAULT '',
  result                 JSON NOT NULL,
  revision_before        BIGINT NOT NULL,
  revision_after         BIGINT NOT NULL,
  rolled_back_by         TEXT NOT NULL DEFAULT '',
  created_at             TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (tenant_id, workspace_id, id)
);

CREATE TABLE ce_provenance (
  tenant_id              TEXT NOT NULL,
  workspace_id           TEXT NOT NULL,
  seq                    BIGINT NOT NULL,
  ingestion_id           TEXT NOT NULL,
  revision               BIGINT NOT NULL,
  entity                 TEXT NOT NULL,
  record_key             TEXT NOT NULL,
  op                     TEXT NOT NULL,
  source_system          TEXT NOT NULL,
  source_adapter         TEXT NOT NULL,
  source_adapter_version TEXT NOT NULL,
  source_record_ref      TEXT NOT NULL DEFAULT '',
  content_hash           TEXT NOT NULL DEFAULT '',
  previous_hash          TEXT NOT NULL DEFAULT '',
  recorded_at            TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (tenant_id, workspace_id, seq)
);
CREATE INDEX ce_provenance_record ON ce_provenance (tenant_id, workspace_id, entity, record_key, seq);

CREATE TABLE ce_manifests (
  tenant_id    TEXT NOT NULL,
  workspace_id TEXT NOT NULL,
  name         TEXT NOT NULL,
  manifest     JSON NOT NULL,
  updated_at   TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (tenant_id, workspace_id, name)
);

CREATE TABLE ce_evaluations (
  tenant_id    TEXT NOT NULL,
  workspace_id TEXT NOT NULL,
  id           TEXT NOT NULL,
  snapshot_id  TEXT NOT NULL,
  catalogs     TEXT[] NOT NULL,
  result       JSON NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (tenant_id, workspace_id, id)
);
