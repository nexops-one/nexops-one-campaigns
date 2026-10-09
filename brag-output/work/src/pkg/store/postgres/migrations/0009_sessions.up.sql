-- Console sessions. Only the SHA-256 hash of the session value is stored.
CREATE TABLE ce_sessions (
  hash         TEXT        PRIMARY KEY,
  tenant_id    TEXT        NOT NULL,
  user_id      TEXT        NOT NULL,
  workspace_id TEXT        NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL,
  last_seen_at TIMESTAMPTZ NOT NULL,
  expires_at   TIMESTAMPTZ NOT NULL,
  FOREIGN KEY (tenant_id, user_id) REFERENCES ce_users (tenant_id, id)
);
CREATE INDEX ce_sessions_tenant ON ce_sessions (tenant_id);
CREATE INDEX ce_sessions_expiry ON ce_sessions (expires_at);
