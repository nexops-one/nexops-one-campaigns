-- Append-only, hash-chained audit log: one chain per (tenant_id, workspace_id);
-- workspace_id '' is the tenant-level chain. details is TEXT so the stored
-- bytes are exactly the hashed bytes.
CREATE TABLE ce_audit_events (
  tenant_id    TEXT        NOT NULL,
  workspace_id TEXT        NOT NULL,
  seq          BIGINT      NOT NULL,
  at           TIMESTAMPTZ NOT NULL,
  actor        TEXT        NOT NULL,
  actor_kind   TEXT        NOT NULL,
  action       TEXT        NOT NULL,
  target_type  TEXT        NOT NULL,
  target_id    TEXT        NOT NULL,
  details      TEXT        NOT NULL,
  prev_hash    TEXT        NOT NULL,
  hash         TEXT        NOT NULL,
  PRIMARY KEY (tenant_id, workspace_id, seq)
);

CREATE FUNCTION ce_audit_append_only() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'ce_audit_events is append-only';
END
$$;

CREATE TRIGGER ce_audit_events_no_change BEFORE UPDATE OR DELETE ON ce_audit_events
  FOR EACH ROW EXECUTE FUNCTION ce_audit_append_only();
CREATE TRIGGER ce_audit_events_no_truncate BEFORE TRUNCATE ON ce_audit_events
  FOR EACH STATEMENT EXECUTE FUNCTION ce_audit_append_only();
