-- Retention may delete the oldest audit events, and only them: a DELETE is
-- allowed when the session sets compliance.audit_prune = 'on' and the row is
-- older than compliance.audit_prune_before. UPDATE and TRUNCATE stay refused.
CREATE OR REPLACE FUNCTION ce_audit_append_only() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE'
     AND coalesce(current_setting('compliance.audit_prune', true), '') = 'on'
     AND OLD.at < current_setting('compliance.audit_prune_before', true)::timestamptz THEN
    RETURN OLD;
  END IF;
  RAISE EXCEPTION 'ce_audit_events is append-only';
END
$$;
