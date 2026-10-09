-- Tenant deletion may remove one tenant's assessment history: a DELETE is
-- allowed only when the session names that tenant in compliance.tenant_delete.
CREATE OR REPLACE FUNCTION ce_append_only() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' AND OLD.tenant_id = current_setting('compliance.tenant_delete', true) THEN
    RETURN OLD;
  END IF;
  RAISE EXCEPTION '% is append-only', TG_TABLE_NAME;
END
$$;
