-- +goose Up
-- +goose StatementBegin

-- NOTE: SQLite has no ALTER COLUMN, so making the author columns nullable means rebuilding both tables; nothing references either table, so the drop cascades nowhere.
CREATE TABLE {{.TablePrefix}}period_closings_new (
  id                  TEXT PRIMARY KEY,
  org_id              TEXT NOT NULL REFERENCES {{.TablePrefix}}orgs(id) ON DELETE CASCADE,
  project_id          TEXT NOT NULL REFERENCES {{.TablePrefix}}projects(id) ON DELETE CASCADE,
  period_id           TEXT NOT NULL,
  closed_at           TEXT NOT NULL,
  closed_by           TEXT,
  closed_by_key_id    TEXT REFERENCES {{.TablePrefix}}api_keys(id) ON DELETE RESTRICT,
  snapshot            TEXT NOT NULL,
  FOREIGN KEY (period_id, org_id) REFERENCES {{.TablePrefix}}periods(id, org_id) ON DELETE CASCADE,
  CONSTRAINT {{.TablePrefix}}period_closings_author_one CHECK ((closed_by IS NULL) <> (closed_by_key_id IS NULL))
);

INSERT INTO {{.TablePrefix}}period_closings_new (id, org_id, project_id, period_id, closed_at, closed_by, closed_by_key_id, snapshot)
SELECT id, org_id, project_id, period_id, closed_at, closed_by, NULL, snapshot
FROM {{.TablePrefix}}period_closings;

DROP TABLE {{.TablePrefix}}period_closings;
ALTER TABLE {{.TablePrefix}}period_closings_new RENAME TO {{.TablePrefix}}period_closings;

CREATE INDEX {{.TablePrefix}}period_closings_period_idx
  ON {{.TablePrefix}}period_closings (period_id, closed_at DESC);

CREATE TABLE {{.TablePrefix}}transactions_new (
  id                  TEXT PRIMARY KEY,
  org_id              TEXT NOT NULL REFERENCES {{.TablePrefix}}orgs(id) ON DELETE CASCADE,
  project_id          TEXT NOT NULL REFERENCES {{.TablePrefix}}projects(id) ON DELETE CASCADE,
  wallet_id           TEXT NOT NULL,
  to_wallet_id        TEXT,
  kind                TEXT NOT NULL CHECK (kind IN ('income','expense','transfer','opening','adjustment_in','adjustment_out')),
  amount_minor        INTEGER NOT NULL CHECK (amount_minor > 0),
  currency            TEXT NOT NULL CHECK (length(currency) = 3),
  category_id         TEXT,
  period_id           TEXT,
  note                TEXT NOT NULL DEFAULT '',
  note_norm           TEXT NOT NULL DEFAULT '',
  occurred_at         TEXT NOT NULL,
  created_by          TEXT,
  created_by_key_id   TEXT REFERENCES {{.TablePrefix}}api_keys(id) ON DELETE RESTRICT,
  created_at          TEXT NOT NULL,
  updated_at          TEXT NOT NULL,
  -- NOTE: composite FKs stop a row from naming another org's wallet, category or period.
  FOREIGN KEY (wallet_id, org_id)    REFERENCES {{.TablePrefix}}wallets(id, org_id)    ON DELETE RESTRICT,
  FOREIGN KEY (to_wallet_id, org_id) REFERENCES {{.TablePrefix}}wallets(id, org_id)    ON DELETE RESTRICT,
  FOREIGN KEY (category_id, org_id)  REFERENCES {{.TablePrefix}}categories(id, org_id) ON DELETE RESTRICT,
  FOREIGN KEY (period_id, org_id)    REFERENCES {{.TablePrefix}}periods(id, org_id),
  CHECK (kind <> 'transfer' OR (to_wallet_id IS NOT NULL AND to_wallet_id <> wallet_id AND category_id IS NULL)),
  CHECK (kind = 'transfer' OR to_wallet_id IS NULL),
  CHECK (kind IN ('income','expense') OR category_id IS NULL),
  CONSTRAINT {{.TablePrefix}}transactions_author_one CHECK ((created_by IS NULL) <> (created_by_key_id IS NULL))
);

INSERT INTO {{.TablePrefix}}transactions_new (id, org_id, project_id, wallet_id, to_wallet_id, kind, amount_minor, currency, category_id, period_id, note, note_norm, occurred_at, created_by, created_by_key_id, created_at, updated_at)
SELECT id, org_id, project_id, wallet_id, to_wallet_id, kind, amount_minor, currency, category_id, period_id, note, note_norm, occurred_at, created_by, NULL, created_at, updated_at
FROM {{.TablePrefix}}transactions;

DROP TABLE {{.TablePrefix}}transactions;
ALTER TABLE {{.TablePrefix}}transactions_new RENAME TO {{.TablePrefix}}transactions;

CREATE INDEX {{.TablePrefix}}transactions_project_occurred_idx
  ON {{.TablePrefix}}transactions (org_id, project_id, occurred_at DESC, created_at DESC, id DESC);
CREATE INDEX {{.TablePrefix}}transactions_wallet_idx     ON {{.TablePrefix}}transactions (wallet_id);
CREATE INDEX {{.TablePrefix}}transactions_to_wallet_idx  ON {{.TablePrefix}}transactions (to_wallet_id);
CREATE INDEX {{.TablePrefix}}transactions_period_idx     ON {{.TablePrefix}}transactions (period_id);
CREATE INDEX {{.TablePrefix}}transactions_category_idx   ON {{.TablePrefix}}transactions (category_id);
CREATE INDEX {{.TablePrefix}}transactions_note_norm_idx  ON {{.TablePrefix}}transactions (project_id, note_norm, occurred_at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- NOTE: key-authored rows (author NULL) cannot satisfy the restored NOT NULL, so the insert fails rather than drop ledger rows.
CREATE TABLE {{.TablePrefix}}transactions_old (
  id                  TEXT PRIMARY KEY,
  org_id              TEXT NOT NULL REFERENCES {{.TablePrefix}}orgs(id) ON DELETE CASCADE,
  project_id          TEXT NOT NULL REFERENCES {{.TablePrefix}}projects(id) ON DELETE CASCADE,
  wallet_id           TEXT NOT NULL,
  to_wallet_id        TEXT,
  kind                TEXT NOT NULL CHECK (kind IN ('income','expense','transfer','opening','adjustment_in','adjustment_out')),
  amount_minor        INTEGER NOT NULL CHECK (amount_minor > 0),
  currency            TEXT NOT NULL CHECK (length(currency) = 3),
  category_id         TEXT,
  period_id           TEXT,
  note                TEXT NOT NULL DEFAULT '',
  note_norm           TEXT NOT NULL DEFAULT '',
  occurred_at         TEXT NOT NULL,
  created_by          TEXT NOT NULL,
  created_at          TEXT NOT NULL,
  updated_at          TEXT NOT NULL,
  -- NOTE: composite FKs stop a row from naming another org's wallet, category or period.
  FOREIGN KEY (wallet_id, org_id)    REFERENCES {{.TablePrefix}}wallets(id, org_id)    ON DELETE RESTRICT,
  FOREIGN KEY (to_wallet_id, org_id) REFERENCES {{.TablePrefix}}wallets(id, org_id)    ON DELETE RESTRICT,
  FOREIGN KEY (category_id, org_id)  REFERENCES {{.TablePrefix}}categories(id, org_id) ON DELETE RESTRICT,
  FOREIGN KEY (period_id, org_id)    REFERENCES {{.TablePrefix}}periods(id, org_id),
  CHECK (kind <> 'transfer' OR (to_wallet_id IS NOT NULL AND to_wallet_id <> wallet_id AND category_id IS NULL)),
  CHECK (kind = 'transfer' OR to_wallet_id IS NULL),
  CHECK (kind IN ('income','expense') OR category_id IS NULL)
);

INSERT INTO {{.TablePrefix}}transactions_old (id, org_id, project_id, wallet_id, to_wallet_id, kind, amount_minor, currency, category_id, period_id, note, note_norm, occurred_at, created_by, created_at, updated_at)
SELECT id, org_id, project_id, wallet_id, to_wallet_id, kind, amount_minor, currency, category_id, period_id, note, note_norm, occurred_at, created_by, created_at, updated_at
FROM {{.TablePrefix}}transactions;

DROP TABLE {{.TablePrefix}}transactions;
ALTER TABLE {{.TablePrefix}}transactions_old RENAME TO {{.TablePrefix}}transactions;

CREATE INDEX {{.TablePrefix}}transactions_project_occurred_idx
  ON {{.TablePrefix}}transactions (org_id, project_id, occurred_at DESC, created_at DESC, id DESC);
CREATE INDEX {{.TablePrefix}}transactions_wallet_idx     ON {{.TablePrefix}}transactions (wallet_id);
CREATE INDEX {{.TablePrefix}}transactions_to_wallet_idx  ON {{.TablePrefix}}transactions (to_wallet_id);
CREATE INDEX {{.TablePrefix}}transactions_period_idx     ON {{.TablePrefix}}transactions (period_id);
CREATE INDEX {{.TablePrefix}}transactions_category_idx   ON {{.TablePrefix}}transactions (category_id);
CREATE INDEX {{.TablePrefix}}transactions_note_norm_idx  ON {{.TablePrefix}}transactions (project_id, note_norm, occurred_at DESC);

CREATE TABLE {{.TablePrefix}}period_closings_old (
  id                  TEXT PRIMARY KEY,
  org_id              TEXT NOT NULL REFERENCES {{.TablePrefix}}orgs(id) ON DELETE CASCADE,
  project_id          TEXT NOT NULL REFERENCES {{.TablePrefix}}projects(id) ON DELETE CASCADE,
  period_id           TEXT NOT NULL,
  closed_at           TEXT NOT NULL,
  closed_by           TEXT NOT NULL,
  snapshot            TEXT NOT NULL,
  FOREIGN KEY (period_id, org_id) REFERENCES {{.TablePrefix}}periods(id, org_id) ON DELETE CASCADE
);

INSERT INTO {{.TablePrefix}}period_closings_old (id, org_id, project_id, period_id, closed_at, closed_by, snapshot)
SELECT id, org_id, project_id, period_id, closed_at, closed_by, snapshot
FROM {{.TablePrefix}}period_closings;

DROP TABLE {{.TablePrefix}}period_closings;
ALTER TABLE {{.TablePrefix}}period_closings_old RENAME TO {{.TablePrefix}}period_closings;

CREATE INDEX {{.TablePrefix}}period_closings_period_idx
  ON {{.TablePrefix}}period_closings (period_id, closed_at DESC);

-- +goose StatementEnd
