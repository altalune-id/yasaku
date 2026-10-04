-- +goose Up
-- +goose StatementBegin

CREATE TABLE {{.TablePrefix}}opensheet_links (
  id                  TEXT PRIMARY KEY,
  org_id              TEXT NOT NULL REFERENCES {{.TablePrefix}}orgs(id) ON DELETE CASCADE,
  project_id          TEXT NOT NULL REFERENCES {{.TablePrefix}}projects(id) ON DELETE CASCADE,
  os_org              TEXT NOT NULL,
  os_project          TEXT NOT NULL,
  api_key_sealed      BLOB NOT NULL CHECK (length(api_key_sealed) > 0),
  api_key_hint        TEXT NOT NULL DEFAULT '',
  transactions_sheet  TEXT NOT NULL,
  wallets_sheet       TEXT NOT NULL,
  categories_sheet    TEXT NOT NULL,
  enabled             INTEGER NOT NULL DEFAULT 0,
  verified_at         TEXT,
  last_error          TEXT NOT NULL DEFAULT '',
  last_synced_at      TEXT,
  failure_streak      INTEGER NOT NULL DEFAULT 0 CHECK (failure_streak >= 0),
  auto_disabled_at    TEXT,
  created_by          TEXT,
  created_by_key_id   TEXT REFERENCES {{.TablePrefix}}api_keys(id) ON DELETE RESTRICT,
  created_at          TEXT NOT NULL,
  updated_at          TEXT NOT NULL,
  UNIQUE (project_id),
  UNIQUE (project_id, org_id),
  CONSTRAINT {{.TablePrefix}}opensheet_links_author_one CHECK ((created_by IS NULL) <> (created_by_key_id IS NULL)),
  CONSTRAINT {{.TablePrefix}}opensheet_links_sheets_distinct CHECK (
    transactions_sheet <> wallets_sheet AND transactions_sheet <> categories_sheet AND wallets_sheet <> categories_sheet)
);
CREATE INDEX {{.TablePrefix}}opensheet_links_enabled_idx
  ON {{.TablePrefix}}opensheet_links (org_id) WHERE enabled = 1;

CREATE TABLE {{.TablePrefix}}opensheet_sync_state (
  org_id          TEXT NOT NULL REFERENCES {{.TablePrefix}}orgs(id) ON DELETE CASCADE,
  project_id      TEXT NOT NULL,
  entity          TEXT NOT NULL CHECK (entity IN ('transaction','wallet','category')),
  entity_id       TEXT NOT NULL,
  version         INTEGER NOT NULL CHECK (version > 0),
  synced_version  INTEGER NOT NULL DEFAULT 0 CHECK (synced_version >= 0),
  deleted         INTEGER NOT NULL DEFAULT 0,
  attempts        INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  last_error      TEXT NOT NULL DEFAULT '',
  lease_token     TEXT,
  leased_until    TEXT,
  retry_after     TEXT,
  updated_at      TEXT NOT NULL,
  PRIMARY KEY (org_id, entity, entity_id),
  FOREIGN KEY (project_id, org_id) REFERENCES {{.TablePrefix}}opensheet_links(project_id, org_id) ON DELETE CASCADE
);
CREATE INDEX {{.TablePrefix}}opensheet_sync_state_link_idx
  ON {{.TablePrefix}}opensheet_sync_state (org_id, project_id);
CREATE INDEX {{.TablePrefix}}opensheet_sync_state_dirty_idx
  ON {{.TablePrefix}}opensheet_sync_state (org_id, project_id, updated_at) WHERE synced_version < version;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS {{.TablePrefix}}opensheet_sync_state;
DROP TABLE IF EXISTS {{.TablePrefix}}opensheet_links;
-- +goose StatementEnd
