-- Reports are immutable. The facts and validation documents are kept byte for
-- byte (BYTEA, not JSONB) because the facts hash is computed over them.
CREATE TABLE ce_reports (
  tenant_id    TEXT        NOT NULL,
  workspace_id TEXT        NOT NULL,
  id           TEXT        NOT NULL,
  doc          JSONB       NOT NULL,
  facts        BYTEA       NOT NULL,
  validation   BYTEA,
  created_at   TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (tenant_id, workspace_id, id)
);
CREATE INDEX ce_reports_created ON ce_reports (tenant_id, workspace_id, created_at DESC, id);

CREATE TABLE ce_report_files (
  tenant_id    TEXT  NOT NULL,
  workspace_id TEXT  NOT NULL,
  report_id    TEXT  NOT NULL,
  name         TEXT  NOT NULL,
  content_type TEXT  NOT NULL,
  sha256       TEXT  NOT NULL,
  content      BYTEA NOT NULL,
  PRIMARY KEY (tenant_id, workspace_id, report_id, name),
  FOREIGN KEY (tenant_id, workspace_id, report_id) REFERENCES ce_reports (tenant_id, workspace_id, id)
);

-- UPDATE is always refused. DELETE is allowed only for retention
-- (compliance.retention = 'on') and for the deletion of the row's tenant.
CREATE FUNCTION ce_report_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' AND (coalesce(current_setting('compliance.retention', true), '') = 'on'
                           OR OLD.tenant_id = current_setting('compliance.tenant_delete', true)) THEN
    RETURN OLD;
  END IF;
  RAISE EXCEPTION '% is immutable', TG_TABLE_NAME;
END
$$;

CREATE TRIGGER ce_reports_immutable BEFORE UPDATE OR DELETE ON ce_reports
  FOR EACH ROW EXECUTE FUNCTION ce_report_immutable();
CREATE TRIGGER ce_reports_no_truncate BEFORE TRUNCATE ON ce_reports
  FOR EACH STATEMENT EXECUTE FUNCTION ce_report_immutable();
CREATE TRIGGER ce_report_files_immutable BEFORE UPDATE OR DELETE ON ce_report_files
  FOR EACH ROW EXECUTE FUNCTION ce_report_immutable();
CREATE TRIGGER ce_report_files_no_truncate BEFORE TRUNCATE ON ce_report_files
  FOR EACH STATEMENT EXECUTE FUNCTION ce_report_immutable();
