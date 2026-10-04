-- +goose Up
-- +goose StatementBegin

-- NOTE: projects has no (id, org_id) key, so project_id references projects(id) as 007_yasaku does; the state rows carry the composite key to the link instead.
CREATE TABLE {{.Schema}}.{{.TablePrefix}}opensheet_links (
  id                  UUID PRIMARY KEY,
  org_id              UUID NOT NULL REFERENCES {{.Schema}}.{{.TablePrefix}}orgs(id) ON DELETE CASCADE,
  project_id          UUID NOT NULL REFERENCES {{.Schema}}.{{.TablePrefix}}projects(id) ON DELETE CASCADE,
  os_org              TEXT NOT NULL,
  os_project          TEXT NOT NULL,
  api_key_sealed      BYTEA NOT NULL CHECK (length(api_key_sealed) > 0),
  api_key_hint        TEXT NOT NULL DEFAULT '',
  transactions_sheet  TEXT NOT NULL,
  wallets_sheet       TEXT NOT NULL,
  categories_sheet    TEXT NOT NULL,
  enabled             BOOLEAN NOT NULL DEFAULT false,
  verified_at         TIMESTAMPTZ,
  last_error          TEXT NOT NULL DEFAULT '',
  last_synced_at      TIMESTAMPTZ,
  failure_streak      INTEGER NOT NULL DEFAULT 0 CHECK (failure_streak >= 0),
  auto_disabled_at    TIMESTAMPTZ,
  created_by          UUID,
  created_by_key_id   UUID REFERENCES {{.Schema}}.{{.TablePrefix}}api_keys(id) ON DELETE RESTRICT,
  created_at          TIMESTAMPTZ NOT NULL,
  updated_at          TIMESTAMPTZ NOT NULL,
  UNIQUE (project_id),
  UNIQUE (project_id, org_id),
  CONSTRAINT {{.TablePrefix}}opensheet_links_author_one CHECK ((created_by IS NULL) <> (created_by_key_id IS NULL)),
  CONSTRAINT {{.TablePrefix}}opensheet_links_sheets_distinct CHECK (
    transactions_sheet <> wallets_sheet AND transactions_sheet <> categories_sheet AND wallets_sheet <> categories_sheet)
);
CREATE INDEX {{.TablePrefix}}opensheet_links_enabled_idx
  ON {{.Schema}}.{{.TablePrefix}}opensheet_links (org_id) WHERE enabled;

CREATE TABLE {{.Schema}}.{{.TablePrefix}}opensheet_sync_state (
  org_id          UUID NOT NULL REFERENCES {{.Schema}}.{{.TablePrefix}}orgs(id) ON DELETE CASCADE,
  project_id      UUID NOT NULL,
  entity          TEXT NOT NULL CHECK (entity IN ('transaction','wallet','category')),
  entity_id       UUID NOT NULL,
  version         BIGINT NOT NULL CHECK (version > 0),
  synced_version  BIGINT NOT NULL DEFAULT 0 CHECK (synced_version >= 0),
  deleted         BOOLEAN NOT NULL DEFAULT false,
  attempts        INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  last_error      TEXT NOT NULL DEFAULT '',
  lease_token     UUID,
  leased_until    TIMESTAMPTZ,
  retry_after     TIMESTAMPTZ,
  updated_at      TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (org_id, entity, entity_id),
  FOREIGN KEY (project_id, org_id) REFERENCES {{.Schema}}.{{.TablePrefix}}opensheet_links(project_id, org_id) ON DELETE CASCADE
);
CREATE INDEX {{.TablePrefix}}opensheet_sync_state_link_idx
  ON {{.Schema}}.{{.TablePrefix}}opensheet_sync_state (org_id, project_id);
CREATE INDEX {{.TablePrefix}}opensheet_sync_state_dirty_idx
  ON {{.Schema}}.{{.TablePrefix}}opensheet_sync_state (org_id, project_id, updated_at) WHERE synced_version < version;

{{if .RLSEnforce}}
ALTER TABLE {{.Schema}}.{{.TablePrefix}}opensheet_links ENABLE ROW LEVEL SECURITY;
ALTER TABLE {{.Schema}}.{{.TablePrefix}}opensheet_links FORCE ROW LEVEL SECURITY;
CREATE POLICY {{.TablePrefix}}opensheet_links_tenant
  ON {{.Schema}}.{{.TablePrefix}}opensheet_links
  USING (org_id = {{.Schema}}.{{.TablePrefix}}current_org_id());

ALTER TABLE {{.Schema}}.{{.TablePrefix}}opensheet_sync_state ENABLE ROW LEVEL SECURITY;
ALTER TABLE {{.Schema}}.{{.TablePrefix}}opensheet_sync_state FORCE ROW LEVEL SECURITY;
CREATE POLICY {{.TablePrefix}}opensheet_sync_state_tenant
  ON {{.Schema}}.{{.TablePrefix}}opensheet_sync_state
  USING (org_id = {{.Schema}}.{{.TablePrefix}}current_org_id());
{{end}}

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS {{.Schema}}.{{.TablePrefix}}opensheet_sync_state;
DROP TABLE IF EXISTS {{.Schema}}.{{.TablePrefix}}opensheet_links;
-- +goose StatementEnd
