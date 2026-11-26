-- +goose Up
-- +goose StatementBegin

CREATE TABLE {{.TablePrefix}}opensheet_grants (
  org_id             TEXT NOT NULL REFERENCES {{.TablePrefix}}orgs(id) ON DELETE CASCADE,
  project_id         TEXT NOT NULL REFERENCES {{.TablePrefix}}projects(id) ON DELETE CASCADE,
  connected_by       TEXT NOT NULL,
  refresh_sealed     BLOB NOT NULL CHECK (length(refresh_sealed) > 0),
  access_sealed      BLOB,
  access_expires_at  TEXT,
  scopes             TEXT NOT NULL,
  status             TEXT NOT NULL CHECK (status IN ('ok','reconnect_needed')),
  created_at         TEXT NOT NULL,
  updated_at         TEXT NOT NULL,
  PRIMARY KEY (project_id),
  UNIQUE (project_id, org_id)
);
CREATE INDEX {{.TablePrefix}}opensheet_grants_connected_by_idx
  ON {{.TablePrefix}}opensheet_grants (org_id, connected_by);

-- NOTE: SQLite cannot alter a NOT NULL or a CHECK, so opensheet_links is rebuilt. goose runs this in a transaction, where PRAGMA foreign_keys cannot be turned off, so dropping the old table cascades to opensheet_sync_state: copy it out first and put it back after.
CREATE TABLE {{.TablePrefix}}opensheet_sync_state_keep AS SELECT * FROM {{.TablePrefix}}opensheet_sync_state;

CREATE TABLE {{.TablePrefix}}opensheet_links_new (
  id                  TEXT PRIMARY KEY,
  org_id              TEXT NOT NULL REFERENCES {{.TablePrefix}}orgs(id) ON DELETE CASCADE,
  project_id          TEXT NOT NULL REFERENCES {{.TablePrefix}}projects(id) ON DELETE CASCADE,
  os_org              TEXT NOT NULL,
  os_project          TEXT NOT NULL,
  api_key_sealed      BLOB,
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
  credential          TEXT NOT NULL DEFAULT 'api_key' CHECK (credential IN ('api_key','grant')),
  UNIQUE (project_id),
  UNIQUE (project_id, org_id),
  CONSTRAINT {{.TablePrefix}}opensheet_links_author_one CHECK ((created_by IS NULL) <> (created_by_key_id IS NULL)),
  CONSTRAINT {{.TablePrefix}}opensheet_links_sheets_distinct CHECK (
    transactions_sheet <> wallets_sheet AND transactions_sheet <> categories_sheet AND wallets_sheet <> categories_sheet),
  CONSTRAINT {{.TablePrefix}}opensheet_links_key_check CHECK (credential = 'grant' OR COALESCE(length(api_key_sealed), 0) > 0)
);
INSERT INTO {{.TablePrefix}}opensheet_links_new (id, org_id, project_id, os_org, os_project, api_key_sealed, api_key_hint,
  transactions_sheet, wallets_sheet, categories_sheet, enabled, verified_at, last_error, last_synced_at, failure_streak,
  auto_disabled_at, created_by, created_by_key_id, created_at, updated_at, credential)
SELECT id, org_id, project_id, os_org, os_project, api_key_sealed, api_key_hint,
  transactions_sheet, wallets_sheet, categories_sheet, enabled, verified_at, last_error, last_synced_at, failure_streak,
  auto_disabled_at, created_by, created_by_key_id, created_at, updated_at, 'api_key'
FROM {{.TablePrefix}}opensheet_links;
DROP TABLE {{.TablePrefix}}opensheet_links;
ALTER TABLE {{.TablePrefix}}opensheet_links_new RENAME TO {{.TablePrefix}}opensheet_links;
CREATE INDEX {{.TablePrefix}}opensheet_links_enabled_idx
  ON {{.TablePrefix}}opensheet_links (org_id) WHERE enabled = 1;

INSERT INTO {{.TablePrefix}}opensheet_sync_state SELECT * FROM {{.TablePrefix}}opensheet_sync_state_keep;
DROP TABLE {{.TablePrefix}}opensheet_sync_state_keep;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- NOTE: a grant link has no key to fall back on and cannot satisfy the restored NOT NULL, so it is dropped with its sync state.
DELETE FROM {{.TablePrefix}}opensheet_links WHERE credential = 'grant';
CREATE TABLE {{.TablePrefix}}opensheet_sync_state_keep AS SELECT * FROM {{.TablePrefix}}opensheet_sync_state;

CREATE TABLE {{.TablePrefix}}opensheet_links_old (
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
INSERT INTO {{.TablePrefix}}opensheet_links_old (id, org_id, project_id, os_org, os_project, api_key_sealed, api_key_hint,
  transactions_sheet, wallets_sheet, categories_sheet, enabled, verified_at, last_error, last_synced_at, failure_streak,
  auto_disabled_at, created_by, created_by_key_id, created_at, updated_at)
SELECT id, org_id, project_id, os_org, os_project, api_key_sealed, api_key_hint,
  transactions_sheet, wallets_sheet, categories_sheet, enabled, verified_at, last_error, last_synced_at, failure_streak,
  auto_disabled_at, created_by, created_by_key_id, created_at, updated_at
FROM {{.TablePrefix}}opensheet_links;
DROP TABLE {{.TablePrefix}}opensheet_links;
ALTER TABLE {{.TablePrefix}}opensheet_links_old RENAME TO {{.TablePrefix}}opensheet_links;
CREATE INDEX {{.TablePrefix}}opensheet_links_enabled_idx
  ON {{.TablePrefix}}opensheet_links (org_id) WHERE enabled = 1;

INSERT INTO {{.TablePrefix}}opensheet_sync_state SELECT * FROM {{.TablePrefix}}opensheet_sync_state_keep;
DROP TABLE {{.TablePrefix}}opensheet_sync_state_keep;

DROP TABLE IF EXISTS {{.TablePrefix}}opensheet_grants;

-- +goose StatementEnd
