-- +goose Up
-- +goose StatementBegin

CREATE TABLE {{.Schema}}.{{.TablePrefix}}ledger_settings (
  project_id        UUID PRIMARY KEY REFERENCES {{.Schema}}.{{.TablePrefix}}projects(id) ON DELETE CASCADE,
  org_id            UUID NOT NULL REFERENCES {{.Schema}}.{{.TablePrefix}}orgs(id) ON DELETE CASCADE,
  timezone          TEXT NOT NULL,
  currency          TEXT NOT NULL CHECK (length(currency) = 3),
  period_start_day  SMALLINT NOT NULL CHECK (period_start_day BETWEEN 1 AND 28),
  updated_at        TIMESTAMPTZ NOT NULL
);

CREATE TABLE {{.Schema}}.{{.TablePrefix}}wallets (
  id                  UUID PRIMARY KEY,
  org_id              UUID NOT NULL REFERENCES {{.Schema}}.{{.TablePrefix}}orgs(id) ON DELETE CASCADE,
  project_id          UUID NOT NULL REFERENCES {{.Schema}}.{{.TablePrefix}}projects(id) ON DELETE CASCADE,
  name                TEXT NOT NULL,
  kind                TEXT NOT NULL CHECK (kind IN ('cash','bank','ewallet','savings','investment','other')),
  provider            TEXT NOT NULL DEFAULT '',
  currency            TEXT NOT NULL CHECK (length(currency) = 3),
  exclude_from_total  BOOLEAN NOT NULL DEFAULT false,
  archived_at         TIMESTAMPTZ,
  created_at          TIMESTAMPTZ NOT NULL,
  updated_at          TIMESTAMPTZ NOT NULL,
  UNIQUE (id, org_id)
);
CREATE UNIQUE INDEX {{.TablePrefix}}wallets_project_name_active
  ON {{.Schema}}.{{.TablePrefix}}wallets (project_id, lower(name)) WHERE archived_at IS NULL;

CREATE TABLE {{.Schema}}.{{.TablePrefix}}categories (
  id                  UUID PRIMARY KEY,
  org_id              UUID NOT NULL REFERENCES {{.Schema}}.{{.TablePrefix}}orgs(id) ON DELETE CASCADE,
  project_id          UUID NOT NULL REFERENCES {{.Schema}}.{{.TablePrefix}}projects(id) ON DELETE CASCADE,
  name                TEXT NOT NULL,
  kind                TEXT NOT NULL CHECK (kind IN ('expense','income')),
  icon                TEXT NOT NULL DEFAULT '',
  color               TEXT NOT NULL DEFAULT '',
  sort_order          INTEGER NOT NULL DEFAULT 0,
  archived_at         TIMESTAMPTZ,
  created_at          TIMESTAMPTZ NOT NULL,
  updated_at          TIMESTAMPTZ NOT NULL,
  UNIQUE (id, org_id)
);
CREATE UNIQUE INDEX {{.TablePrefix}}categories_project_kind_name_active
  ON {{.Schema}}.{{.TablePrefix}}categories (project_id, kind, lower(name)) WHERE archived_at IS NULL;

CREATE TABLE {{.Schema}}.{{.TablePrefix}}periods (
  id                  UUID PRIMARY KEY,
  org_id              UUID NOT NULL REFERENCES {{.Schema}}.{{.TablePrefix}}orgs(id) ON DELETE CASCADE,
  project_id          UUID NOT NULL REFERENCES {{.Schema}}.{{.TablePrefix}}projects(id) ON DELETE CASCADE,
  name                TEXT NOT NULL,
  start_date          DATE NOT NULL,
  end_date            DATE,
  status              TEXT NOT NULL CHECK (status IN ('open','closed')),
  closed_at           TIMESTAMPTZ,
  snapshot            JSONB,
  created_at          TIMESTAMPTZ NOT NULL,
  updated_at          TIMESTAMPTZ NOT NULL,
  CHECK (end_date IS NULL OR end_date >= start_date),
  CHECK (status = 'open' OR end_date IS NOT NULL),
  UNIQUE (id, org_id)
);
CREATE UNIQUE INDEX {{.TablePrefix}}periods_project_current
  ON {{.Schema}}.{{.TablePrefix}}periods (project_id) WHERE end_date IS NULL;
CREATE INDEX {{.TablePrefix}}periods_project_start_idx
  ON {{.Schema}}.{{.TablePrefix}}periods (org_id, project_id, start_date DESC, id DESC);

CREATE TABLE {{.Schema}}.{{.TablePrefix}}period_closings (
  id                  UUID PRIMARY KEY,
  org_id              UUID NOT NULL REFERENCES {{.Schema}}.{{.TablePrefix}}orgs(id) ON DELETE CASCADE,
  project_id          UUID NOT NULL REFERENCES {{.Schema}}.{{.TablePrefix}}projects(id) ON DELETE CASCADE,
  period_id           UUID NOT NULL,
  closed_at           TIMESTAMPTZ NOT NULL,
  closed_by           UUID NOT NULL,
  snapshot            JSONB NOT NULL,
  FOREIGN KEY (period_id, org_id) REFERENCES {{.Schema}}.{{.TablePrefix}}periods(id, org_id) ON DELETE CASCADE
);
CREATE INDEX {{.TablePrefix}}period_closings_period_idx
  ON {{.Schema}}.{{.TablePrefix}}period_closings (period_id, closed_at DESC);

CREATE TABLE {{.Schema}}.{{.TablePrefix}}transactions (
  id                  UUID PRIMARY KEY,
  org_id              UUID NOT NULL REFERENCES {{.Schema}}.{{.TablePrefix}}orgs(id) ON DELETE CASCADE,
  project_id          UUID NOT NULL REFERENCES {{.Schema}}.{{.TablePrefix}}projects(id) ON DELETE CASCADE,
  wallet_id           UUID NOT NULL,
  to_wallet_id        UUID,
  kind                TEXT NOT NULL CHECK (kind IN ('income','expense','transfer','opening','adjustment_in','adjustment_out')),
  amount_minor        BIGINT NOT NULL CHECK (amount_minor > 0),
  currency            TEXT NOT NULL CHECK (length(currency) = 3),
  category_id         UUID,
  period_id           UUID,
  note                TEXT NOT NULL DEFAULT '',
  note_norm           TEXT NOT NULL DEFAULT '',
  occurred_at         TIMESTAMPTZ NOT NULL,
  created_by          UUID NOT NULL,
  created_at          TIMESTAMPTZ NOT NULL,
  updated_at          TIMESTAMPTZ NOT NULL,
  -- NOTE: composite FKs stop a row from naming another org's wallet, category or period.
  FOREIGN KEY (wallet_id, org_id)    REFERENCES {{.Schema}}.{{.TablePrefix}}wallets(id, org_id)    ON DELETE RESTRICT,
  FOREIGN KEY (to_wallet_id, org_id) REFERENCES {{.Schema}}.{{.TablePrefix}}wallets(id, org_id)    ON DELETE RESTRICT,
  FOREIGN KEY (category_id, org_id)  REFERENCES {{.Schema}}.{{.TablePrefix}}categories(id, org_id) ON DELETE RESTRICT,
  FOREIGN KEY (period_id, org_id)    REFERENCES {{.Schema}}.{{.TablePrefix}}periods(id, org_id),
  CHECK (kind <> 'transfer' OR (to_wallet_id IS NOT NULL AND to_wallet_id <> wallet_id AND category_id IS NULL)),
  CHECK (kind = 'transfer' OR to_wallet_id IS NULL),
  CHECK (kind IN ('income','expense') OR category_id IS NULL)
);
CREATE INDEX {{.TablePrefix}}transactions_project_occurred_idx
  ON {{.Schema}}.{{.TablePrefix}}transactions (org_id, project_id, occurred_at DESC, created_at DESC, id DESC);
CREATE INDEX {{.TablePrefix}}transactions_wallet_idx     ON {{.Schema}}.{{.TablePrefix}}transactions (wallet_id);
CREATE INDEX {{.TablePrefix}}transactions_to_wallet_idx  ON {{.Schema}}.{{.TablePrefix}}transactions (to_wallet_id);
CREATE INDEX {{.TablePrefix}}transactions_period_idx     ON {{.Schema}}.{{.TablePrefix}}transactions (period_id);
CREATE INDEX {{.TablePrefix}}transactions_category_idx   ON {{.Schema}}.{{.TablePrefix}}transactions (category_id);
CREATE INDEX {{.TablePrefix}}transactions_note_norm_idx  ON {{.Schema}}.{{.TablePrefix}}transactions (project_id, note_norm, occurred_at DESC);

{{if .RLSEnforce}}
ALTER TABLE {{.Schema}}.{{.TablePrefix}}ledger_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE {{.Schema}}.{{.TablePrefix}}ledger_settings FORCE ROW LEVEL SECURITY;
CREATE POLICY {{.TablePrefix}}ledger_settings_tenant
  ON {{.Schema}}.{{.TablePrefix}}ledger_settings
  USING (org_id = {{.Schema}}.{{.TablePrefix}}current_org_id());

ALTER TABLE {{.Schema}}.{{.TablePrefix}}wallets ENABLE ROW LEVEL SECURITY;
ALTER TABLE {{.Schema}}.{{.TablePrefix}}wallets FORCE ROW LEVEL SECURITY;
CREATE POLICY {{.TablePrefix}}wallets_tenant
  ON {{.Schema}}.{{.TablePrefix}}wallets
  USING (org_id = {{.Schema}}.{{.TablePrefix}}current_org_id());

ALTER TABLE {{.Schema}}.{{.TablePrefix}}categories ENABLE ROW LEVEL SECURITY;
ALTER TABLE {{.Schema}}.{{.TablePrefix}}categories FORCE ROW LEVEL SECURITY;
CREATE POLICY {{.TablePrefix}}categories_tenant
  ON {{.Schema}}.{{.TablePrefix}}categories
  USING (org_id = {{.Schema}}.{{.TablePrefix}}current_org_id());

ALTER TABLE {{.Schema}}.{{.TablePrefix}}periods ENABLE ROW LEVEL SECURITY;
ALTER TABLE {{.Schema}}.{{.TablePrefix}}periods FORCE ROW LEVEL SECURITY;
CREATE POLICY {{.TablePrefix}}periods_tenant
  ON {{.Schema}}.{{.TablePrefix}}periods
  USING (org_id = {{.Schema}}.{{.TablePrefix}}current_org_id());

ALTER TABLE {{.Schema}}.{{.TablePrefix}}period_closings ENABLE ROW LEVEL SECURITY;
ALTER TABLE {{.Schema}}.{{.TablePrefix}}period_closings FORCE ROW LEVEL SECURITY;
CREATE POLICY {{.TablePrefix}}period_closings_tenant
  ON {{.Schema}}.{{.TablePrefix}}period_closings
  USING (org_id = {{.Schema}}.{{.TablePrefix}}current_org_id());

ALTER TABLE {{.Schema}}.{{.TablePrefix}}transactions ENABLE ROW LEVEL SECURITY;
ALTER TABLE {{.Schema}}.{{.TablePrefix}}transactions FORCE ROW LEVEL SECURITY;
CREATE POLICY {{.TablePrefix}}transactions_tenant
  ON {{.Schema}}.{{.TablePrefix}}transactions
  USING (org_id = {{.Schema}}.{{.TablePrefix}}current_org_id());
{{end}}

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS {{.Schema}}.{{.TablePrefix}}transactions;
DROP TABLE IF EXISTS {{.Schema}}.{{.TablePrefix}}period_closings;
DROP TABLE IF EXISTS {{.Schema}}.{{.TablePrefix}}periods;
DROP TABLE IF EXISTS {{.Schema}}.{{.TablePrefix}}categories;
DROP TABLE IF EXISTS {{.Schema}}.{{.TablePrefix}}wallets;
DROP TABLE IF EXISTS {{.Schema}}.{{.TablePrefix}}ledger_settings;
-- +goose StatementEnd
