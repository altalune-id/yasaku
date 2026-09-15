-- +goose Up
-- +goose StatementBegin

CREATE TABLE {{.TablePrefix}}users (
  id                  TEXT PRIMARY KEY,
  idp_issuer          TEXT,
  idp_subject         TEXT,
  email               TEXT NOT NULL UNIQUE,
  name                TEXT NOT NULL DEFAULT '',
  avatar_url          TEXT NOT NULL DEFAULT '',
  password_hash       TEXT NOT NULL DEFAULT '',
  is_admin            INTEGER NOT NULL DEFAULT 0,
  locale              TEXT NOT NULL DEFAULT '',
  terms_accepted_at   TEXT,
  created_at          TEXT NOT NULL,
  updated_at          TEXT NOT NULL
);

CREATE UNIQUE INDEX {{.TablePrefix}}users_idp_idx
  ON {{.TablePrefix}}users (idp_issuer, idp_subject)
  WHERE idp_issuer IS NOT NULL;

CREATE TABLE {{.TablePrefix}}orgs (
  id                  TEXT PRIMARY KEY,
  slug                TEXT NOT NULL UNIQUE,
  name                TEXT NOT NULL,
  system              INTEGER NOT NULL DEFAULT 0,
  created_by          TEXT NOT NULL REFERENCES {{.TablePrefix}}users(id),
  created_at          TEXT NOT NULL,
  updated_at          TEXT NOT NULL
);

CREATE TABLE {{.TablePrefix}}memberships (
  id                  TEXT PRIMARY KEY,
  org_id              TEXT NOT NULL REFERENCES {{.TablePrefix}}orgs(id) ON DELETE CASCADE,
  user_id             TEXT NOT NULL REFERENCES {{.TablePrefix}}users(id) ON DELETE CASCADE,
  role                TEXT NOT NULL CHECK (role IN ('owner','admin','member')),
  system              INTEGER NOT NULL DEFAULT 0,
  created_at          TEXT NOT NULL,
  UNIQUE (org_id, user_id)
);

CREATE INDEX {{.TablePrefix}}memberships_user_idx
  ON {{.TablePrefix}}memberships (user_id);

CREATE TABLE {{.TablePrefix}}projects (
  id                  TEXT PRIMARY KEY,
  org_id              TEXT NOT NULL REFERENCES {{.TablePrefix}}orgs(id) ON DELETE CASCADE,
  slug                TEXT NOT NULL,
  name                TEXT NOT NULL,
  system              INTEGER NOT NULL DEFAULT 0,
  created_by          TEXT NOT NULL REFERENCES {{.TablePrefix}}users(id),
  created_at          TEXT NOT NULL,
  updated_at          TEXT NOT NULL,
  UNIQUE (org_id, slug)
);

CREATE TABLE {{.TablePrefix}}invites (
  id                  TEXT PRIMARY KEY,
  org_id              TEXT NOT NULL REFERENCES {{.TablePrefix}}orgs(id) ON DELETE CASCADE,
  email               TEXT NOT NULL,
  role                TEXT NOT NULL CHECK (role IN ('owner','admin','member')),
  token_hash          TEXT NOT NULL,
  expires_at          TEXT NOT NULL,
  accepted_at         TEXT,
  invited_by          TEXT NOT NULL REFERENCES {{.TablePrefix}}users(id),
  created_at          TEXT NOT NULL
);

CREATE INDEX {{.TablePrefix}}invites_email_pending_idx
  ON {{.TablePrefix}}invites (email)
  WHERE accepted_at IS NULL;

CREATE TABLE {{.TablePrefix}}todos (
  id                  TEXT PRIMARY KEY,
  org_id              TEXT NOT NULL REFERENCES {{.TablePrefix}}orgs(id) ON DELETE CASCADE,
  project_id          TEXT NOT NULL REFERENCES {{.TablePrefix}}projects(id) ON DELETE CASCADE,
  user_id             TEXT NOT NULL REFERENCES {{.TablePrefix}}users(id),
  title               TEXT NOT NULL,
  done                INTEGER NOT NULL DEFAULT 0,
  created_at          TEXT NOT NULL,
  updated_at          TEXT NOT NULL
);

CREATE INDEX {{.TablePrefix}}todos_org_project_created_idx
  ON {{.TablePrefix}}todos (org_id, project_id, created_at DESC);

-- NOTE: no org_id — a session is resolved before any tenant scope exists.
CREATE TABLE {{.TablePrefix}}sessions (
  sid                 TEXT PRIMARY KEY,
  user_id             TEXT NOT NULL REFERENCES {{.TablePrefix}}users(id) ON DELETE CASCADE,
  -- SECURITY: sealed Principal, AAD-bound to sid — it carries a live IdP ID token.
  payload             BLOB NOT NULL,
  expires_at          TEXT NOT NULL,
  created_at          TEXT NOT NULL
);

CREATE INDEX {{.TablePrefix}}sessions_expires_idx
  ON {{.TablePrefix}}sessions (expires_at);

CREATE INDEX {{.TablePrefix}}sessions_user_idx
  ON {{.TablePrefix}}sessions (user_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS {{.TablePrefix}}sessions_user_idx;
DROP INDEX IF EXISTS {{.TablePrefix}}sessions_expires_idx;
DROP TABLE IF EXISTS {{.TablePrefix}}sessions;
DROP INDEX IF EXISTS {{.TablePrefix}}todos_org_project_created_idx;
DROP TABLE IF EXISTS {{.TablePrefix}}todos;
DROP INDEX IF EXISTS {{.TablePrefix}}invites_email_pending_idx;
DROP TABLE IF EXISTS {{.TablePrefix}}invites;
DROP TABLE IF EXISTS {{.TablePrefix}}projects;
DROP INDEX IF EXISTS {{.TablePrefix}}memberships_user_idx;
DROP TABLE IF EXISTS {{.TablePrefix}}memberships;
DROP TABLE IF EXISTS {{.TablePrefix}}orgs;
DROP INDEX IF EXISTS {{.TablePrefix}}users_idp_idx;
DROP TABLE IF EXISTS {{.TablePrefix}}users;

-- +goose StatementEnd
