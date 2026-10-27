CREATE TABLE ce_users (
  tenant_id     TEXT        NOT NULL,
  id            TEXT        NOT NULL,
  email         TEXT        NOT NULL,
  password_hash TEXT        NOT NULL DEFAULT '',
  disabled      BOOLEAN     NOT NULL DEFAULT false,
  created_at    TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (tenant_id, id),
  UNIQUE (tenant_id, email)
);

CREATE TABLE ce_members (
  tenant_id    TEXT        NOT NULL,
  workspace_id TEXT        NOT NULL,
  user_id      TEXT        NOT NULL,
  roles        TEXT[]      NOT NULL,
  updated_at   TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (tenant_id, workspace_id, user_id),
  FOREIGN KEY (tenant_id, user_id) REFERENCES ce_users (tenant_id, id)
);

CREATE TABLE ce_tokens (
  id           TEXT        PRIMARY KEY,
  tenant_id    TEXT        NOT NULL,
  workspace_id TEXT        NOT NULL,
  hash         TEXT        NOT NULL UNIQUE,
  name         TEXT        NOT NULL,
  user_id      TEXT        NOT NULL DEFAULT '',
  roles        TEXT[]      NOT NULL,
  created_by   TEXT        NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL,
  expires_at   TIMESTAMPTZ NULL,
  revoked_at   TIMESTAMPTZ NULL,
  last_used_at TIMESTAMPTZ NULL
);

CREATE INDEX ce_tokens_scope ON ce_tokens (tenant_id, workspace_id, created_at);
