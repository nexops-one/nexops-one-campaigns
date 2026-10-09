CREATE OR REPLACE FUNCTION ce_audit_append_only() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'ce_audit_events is append-only';
END
$$;
