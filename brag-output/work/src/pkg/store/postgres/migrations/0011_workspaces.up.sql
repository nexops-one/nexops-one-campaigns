-- Workspace registry: lifecycle (active, suspended) and the basis of the
-- optional workspace limit. Every workspace that already holds data,
-- members, tokens or settings is registered as active.
CREATE TABLE ce_workspaces (
  tenant_id    TEXT        NOT NULL,
  workspace_id TEXT        NOT NULL,
  status       TEXT        NOT NULL CHECK (status IN ('active', 'suspended')),
  created_at   TIMESTAMPTZ NOT NULL,
  created_by   TEXT        NOT NULL,
  status_at    TIMESTAMPTZ NOT NULL,
  status_by    TEXT        NOT NULL,
  reason       TEXT        NOT NULL DEFAULT '',
  PRIMARY KEY (tenant_id, workspace_id)
);
CREATE INDEX ce_workspaces_status ON ce_workspaces (status);

INSERT INTO ce_workspaces (tenant_id, workspace_id, status, created_at, created_by, status_at, status_by)
SELECT tenant_id, workspace_id, 'active', now(), 'migration', now(), 'migration' FROM (
  SELECT tenant_id, workspace_id FROM ce_revisions
  UNION SELECT tenant_id, workspace_id FROM ce_manifests
  UNION SELECT tenant_id, workspace_id FROM ce_evidence
  UNION SELECT tenant_id, workspace_id FROM ce_assessments
  UNION SELECT tenant_id, workspace_id FROM ce_members
  UNION SELECT tenant_id, workspace_id FROM ce_tokens
  UNION SELECT tenant_id, workspace_id FROM ce_settings
) AS used
WHERE workspace_id <> '';
