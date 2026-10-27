-- Evidence references and assessments are stored as JSON documents with an
-- optimistic version; links and history are separate tables for queries.
CREATE TABLE ce_evidence (
  tenant_id    TEXT        NOT NULL,
  workspace_id TEXT        NOT NULL,
  id           TEXT        NOT NULL,
  doc          JSONB       NOT NULL,
  version      BIGINT      NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (tenant_id, workspace_id, id)
);

CREATE TABLE ce_evidence_links (
  tenant_id    TEXT NOT NULL,
  workspace_id TEXT NOT NULL,
  evidence_id  TEXT NOT NULL,
  catalog      TEXT NOT NULL,
  control_id   TEXT NOT NULL,
  PRIMARY KEY (tenant_id, workspace_id, evidence_id, catalog, control_id),
  FOREIGN KEY (tenant_id, workspace_id, evidence_id) REFERENCES ce_evidence (tenant_id, workspace_id, id)
);
CREATE INDEX ce_evidence_links_control ON ce_evidence_links (tenant_id, workspace_id, catalog, control_id);

CREATE TABLE ce_assessments (
  tenant_id    TEXT   NOT NULL,
  workspace_id TEXT   NOT NULL,
  catalog      TEXT   NOT NULL,
  control_id   TEXT   NOT NULL,
  doc          JSONB  NOT NULL,
  version      BIGINT NOT NULL,
  PRIMARY KEY (tenant_id, workspace_id, catalog, control_id)
);

CREATE TABLE ce_assessment_history (
  tenant_id    TEXT   NOT NULL,
  workspace_id TEXT   NOT NULL,
  seq          BIGINT NOT NULL,
  catalog      TEXT   NOT NULL,
  control_id   TEXT   NOT NULL,
  doc          JSONB  NOT NULL,
  PRIMARY KEY (tenant_id, workspace_id, seq)
);
CREATE INDEX ce_assessment_history_control ON ce_assessment_history (tenant_id, workspace_id, catalog, control_id, seq);

CREATE FUNCTION ce_append_only() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION '% is append-only', TG_TABLE_NAME;
END
$$;

CREATE TRIGGER ce_assessment_history_no_change BEFORE UPDATE OR DELETE ON ce_assessment_history
  FOR EACH ROW EXECUTE FUNCTION ce_append_only();
CREATE TRIGGER ce_assessment_history_no_truncate BEFORE TRUNCATE ON ce_assessment_history
  FOR EACH STATEMENT EXECUTE FUNCTION ce_append_only();
