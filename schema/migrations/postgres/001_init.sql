-- +goose Up
-- +goose StatementBegin

CREATE TABLE {{.Schema}}.{{.TablePrefix}}users (
  id                  UUID PRIMARY KEY,
  idp_issuer          TEXT,
  idp_subject         TEXT,
  email               TEXT NOT NULL UNIQUE,
  name                TEXT NOT NULL DEFAULT '',
  avatar_url          TEXT NOT NULL DEFAULT '',
  password_hash       TEXT NOT NULL DEFAULT '',
  is_admin            BOOLEAN NOT NULL DEFAULT FALSE,
  locale              TEXT NOT NULL DEFAULT '',
  terms_accepted_at   TIMESTAMPTZ,
  created_at          TIMESTAMPTZ NOT NULL,
  updated_at          TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX {{.TablePrefix}}users_idp_idx
  ON {{.Schema}}.{{.TablePrefix}}users (idp_issuer, idp_subject)
  WHERE idp_issuer IS NOT NULL;

CREATE TABLE {{.Schema}}.{{.TablePrefix}}orgs (
  id                  UUID PRIMARY KEY,
  slug                TEXT NOT NULL UNIQUE,
  name                TEXT NOT NULL,
  system              BOOLEAN NOT NULL DEFAULT FALSE,
  created_by          UUID NOT NULL REFERENCES {{.Schema}}.{{.TablePrefix}}users(id),
  created_at          TIMESTAMPTZ NOT NULL,
  updated_at          TIMESTAMPTZ NOT NULL
);

CREATE TABLE {{.Schema}}.{{.TablePrefix}}memberships (
  id                  UUID PRIMARY KEY,
  org_id              UUID NOT NULL REFERENCES {{.Schema}}.{{.TablePrefix}}orgs(id) ON DELETE CASCADE,
  user_id             UUID NOT NULL REFERENCES {{.Schema}}.{{.TablePrefix}}users(id) ON DELETE CASCADE,
  role                TEXT NOT NULL CHECK (role IN ('owner','admin','member')),
  system              BOOLEAN NOT NULL DEFAULT FALSE,
  created_at          TIMESTAMPTZ NOT NULL,
  UNIQUE (org_id, user_id)
);

CREATE INDEX {{.TablePrefix}}memberships_user_idx
  ON {{.Schema}}.{{.TablePrefix}}memberships (user_id);

CREATE TABLE {{.Schema}}.{{.TablePrefix}}projects (
  id                  UUID PRIMARY KEY,
  org_id              UUID NOT NULL REFERENCES {{.Schema}}.{{.TablePrefix}}orgs(id) ON DELETE CASCADE,
  slug                TEXT NOT NULL,
  name                TEXT NOT NULL,
  system              BOOLEAN NOT NULL DEFAULT FALSE,
  created_by          UUID NOT NULL REFERENCES {{.Schema}}.{{.TablePrefix}}users(id),
  created_at          TIMESTAMPTZ NOT NULL,
  updated_at          TIMESTAMPTZ NOT NULL,
  UNIQUE (org_id, slug)
);

CREATE TABLE {{.Schema}}.{{.TablePrefix}}invites (
  id                  UUID PRIMARY KEY,
  org_id              UUID NOT NULL REFERENCES {{.Schema}}.{{.TablePrefix}}orgs(id) ON DELETE CASCADE,
  email               TEXT NOT NULL,
  role                TEXT NOT NULL CHECK (role IN ('owner','admin','member')),
  token_hash          TEXT NOT NULL,
  expires_at          TIMESTAMPTZ NOT NULL,
  accepted_at         TIMESTAMPTZ,
  invited_by          UUID NOT NULL REFERENCES {{.Schema}}.{{.TablePrefix}}users(id),
  created_at          TIMESTAMPTZ NOT NULL
);

CREATE INDEX {{.TablePrefix}}invites_email_pending_idx
  ON {{.Schema}}.{{.TablePrefix}}invites (email)
  WHERE accepted_at IS NULL;

CREATE TABLE {{.Schema}}.{{.TablePrefix}}todos (
  id                  UUID PRIMARY KEY,
  org_id              UUID NOT NULL REFERENCES {{.Schema}}.{{.TablePrefix}}orgs(id) ON DELETE CASCADE,
  project_id          UUID NOT NULL REFERENCES {{.Schema}}.{{.TablePrefix}}projects(id) ON DELETE CASCADE,
  user_id             UUID NOT NULL REFERENCES {{.Schema}}.{{.TablePrefix}}users(id),
  title               TEXT NOT NULL,
  done                BOOLEAN NOT NULL DEFAULT FALSE,
  created_at          TIMESTAMPTZ NOT NULL,
  updated_at          TIMESTAMPTZ NOT NULL
);

CREATE INDEX {{.TablePrefix}}todos_org_project_created_idx
  ON {{.Schema}}.{{.TablePrefix}}todos (org_id, project_id, created_at DESC);

-- NOTE: no org_id and no RLS policy — a session is resolved before any tenant scope exists, so a policy here would reject every login under db.allowBypassRLS=false.
CREATE TABLE {{.Schema}}.{{.TablePrefix}}sessions (
  sid                 TEXT PRIMARY KEY,
  user_id             UUID NOT NULL REFERENCES {{.Schema}}.{{.TablePrefix}}users(id) ON DELETE CASCADE,
  -- SECURITY: sealed Principal, AAD-bound to sid — it carries a live IdP ID token.
  payload             BYTEA NOT NULL,
  expires_at          TIMESTAMPTZ NOT NULL,
  created_at          TIMESTAMPTZ NOT NULL
);

CREATE INDEX {{.TablePrefix}}sessions_expires_idx
  ON {{.Schema}}.{{.TablePrefix}}sessions (expires_at);

CREATE INDEX {{.TablePrefix}}sessions_user_idx
  ON {{.Schema}}.{{.TablePrefix}}sessions (user_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS {{.Schema}}.{{.TablePrefix}}sessions_user_idx;
DROP INDEX IF EXISTS {{.Schema}}.{{.TablePrefix}}sessions_expires_idx;
DROP TABLE IF EXISTS {{.Schema}}.{{.TablePrefix}}sessions;
DROP INDEX IF EXISTS {{.Schema}}.{{.TablePrefix}}todos_org_project_created_idx;
DROP TABLE IF EXISTS {{.Schema}}.{{.TablePrefix}}todos;
DROP INDEX IF EXISTS {{.Schema}}.{{.TablePrefix}}invites_email_pending_idx;
DROP TABLE IF EXISTS {{.Schema}}.{{.TablePrefix}}invites;
DROP TABLE IF EXISTS {{.Schema}}.{{.TablePrefix}}projects;
DROP INDEX IF EXISTS {{.Schema}}.{{.TablePrefix}}memberships_user_idx;
DROP TABLE IF EXISTS {{.Schema}}.{{.TablePrefix}}memberships;
DROP TABLE IF EXISTS {{.Schema}}.{{.TablePrefix}}orgs;
DROP INDEX IF EXISTS {{.Schema}}.{{.TablePrefix}}users_idp_idx;
DROP TABLE IF EXISTS {{.Schema}}.{{.TablePrefix}}users;

-- +goose StatementEnd
