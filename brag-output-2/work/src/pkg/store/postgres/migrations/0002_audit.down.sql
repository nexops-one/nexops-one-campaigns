DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM ce_audit_events)
     AND coalesce(current_setting('compliance.force_drop_audit', true), '') <> 'on' THEN
    RAISE EXCEPTION 'ce_audit_events holds audit events; run migrate down with --force-drop-audit to drop them';
  END IF;
END
$$;
DROP TABLE ce_audit_events;
DROP FUNCTION ce_audit_append_only();
