-- +goose Up
-- +goose StatementBegin

CREATE TABLE {{.TablePrefix}}ledger_settings (
  project_id        TEXT PRIMARY KEY REFERENCES {{.TablePrefix}}projects(id) ON DELETE CASCADE,
  org_id            TEXT NOT NULL REFERENCES {{.TablePrefix}}orgs(id) ON DELETE CASCADE,
  timezone          TEXT NOT NULL,
  currency          TEXT NOT NULL CHECK (length(currency) = 3),
  period_start_day  INTEGER NOT NULL CHECK (period_start_day BETWEEN 1 AND 28),
  updated_at        TEXT NOT NULL
);

CREATE TABLE {{.TablePrefix}}wallets (
  id                  TEXT PRIMARY KEY,
  org_id              TEXT NOT NULL REFERENCES {{.TablePrefix}}orgs(id) ON DELETE CASCADE,
  project_id          TEXT NOT NULL REFERENCES {{.TablePrefix}}projects(id) ON DELETE CASCADE,
  name                TEXT NOT NULL,
  kind                TEXT NOT NULL CHECK (kind IN ('cash','bank','ewallet','savings','investment','other')),
  provider            TEXT NOT NULL DEFAULT '',
  currency            TEXT NOT NULL CHECK (length(currency) = 3),
  exclude_from_total  INTEGER NOT NULL DEFAULT 0,
  archived_at         TEXT,
  created_at          TEXT NOT NULL,
  updated_at          TEXT NOT NULL,
  UNIQUE (id, org_id)
);
CREATE UNIQUE INDEX {{.TablePrefix}}wallets_project_name_active
  ON {{.TablePrefix}}wallets (project_id, lower(name)) WHERE archived_at IS NULL;

CREATE TABLE {{.TablePrefix}}categories (
  id                  TEXT PRIMARY KEY,
  org_id              TEXT NOT NULL REFERENCES {{.TablePrefix}}orgs(id) ON DELETE CASCADE,
  project_id          TEXT NOT NULL REFERENCES {{.TablePrefix}}projects(id) ON DELETE CASCADE,
  name                TEXT NOT NULL,
  kind                TEXT NOT NULL CHECK (kind IN ('expense','income')),
  icon                TEXT NOT NULL DEFAULT '',
  color               TEXT NOT NULL DEFAULT '',
  sort_order          INTEGER NOT NULL DEFAULT 0,
  archived_at         TEXT,
  created_at          TEXT NOT NULL,
  updated_at          TEXT NOT NULL,
  UNIQUE (id, org_id)
);
CREATE UNIQUE INDEX {{.TablePrefix}}categories_project_kind_name_active
  ON {{.TablePrefix}}categories (project_id, kind, lower(name)) WHERE archived_at IS NULL;

CREATE TABLE {{.TablePrefix}}periods (
  id                  TEXT PRIMARY KEY,
  org_id              TEXT NOT NULL REFERENCES {{.TablePrefix}}orgs(id) ON DELETE CASCADE,
  project_id          TEXT NOT NULL REFERENCES {{.TablePrefix}}projects(id) ON DELETE CASCADE,
  name                TEXT NOT NULL,
  start_date          TEXT NOT NULL,
  end_date            TEXT,
  status              TEXT NOT NULL CHECK (status IN ('open','closed')),
  closed_at           TEXT,
  snapshot            TEXT,
  created_at          TEXT NOT NULL,
  updated_at          TEXT NOT NULL,
  CHECK (end_date IS NULL OR end_date >= start_date),
  CHECK (status = 'open' OR end_date IS NOT NULL),
  UNIQUE (id, org_id)
);
CREATE UNIQUE INDEX {{.TablePrefix}}periods_project_current
  ON {{.TablePrefix}}periods (project_id) WHERE end_date IS NULL;
CREATE INDEX {{.TablePrefix}}periods_project_start_idx
  ON {{.TablePrefix}}periods (org_id, project_id, start_date DESC, id DESC);

CREATE TABLE {{.TablePrefix}}period_closings (
  id                  TEXT PRIMARY KEY,
  org_id              TEXT NOT NULL REFERENCES {{.TablePrefix}}orgs(id) ON DELETE CASCADE,
  project_id          TEXT NOT NULL REFERENCES {{.TablePrefix}}projects(id) ON DELETE CASCADE,
  period_id           TEXT NOT NULL,
  closed_at           TEXT NOT NULL,
  closed_by           TEXT NOT NULL,
  snapshot            TEXT NOT NULL,
  FOREIGN KEY (period_id, org_id) REFERENCES {{.TablePrefix}}periods(id, org_id) ON DELETE CASCADE
);
CREATE INDEX {{.TablePrefix}}period_closings_period_idx
  ON {{.TablePrefix}}period_closings (period_id, closed_at DESC);

CREATE TABLE {{.TablePrefix}}transactions (
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
DROP TABLE IF EXISTS {{.TablePrefix}}transactions;
DROP TABLE IF EXISTS {{.TablePrefix}}period_closings;
DROP TABLE IF EXISTS {{.TablePrefix}}periods;
DROP TABLE IF EXISTS {{.TablePrefix}}categories;
DROP TABLE IF EXISTS {{.TablePrefix}}wallets;
DROP TABLE IF EXISTS {{.TablePrefix}}ledger_settings;
-- +goose StatementEnd
