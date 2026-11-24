-- +goose Up
-- +goose StatementBegin

ALTER TABLE {{.TablePrefix}}orgs ADD COLUMN plan TEXT NOT NULL DEFAULT 'first';

CREATE TABLE {{.TablePrefix}}plan_limits (
  plan        TEXT NOT NULL,
  key         TEXT NOT NULL,
  value       INTEGER CHECK (value IS NULL OR value >= 0),
  updated_at  TEXT NOT NULL,
  updated_by  TEXT,
  PRIMARY KEY (plan, key)
);

CREATE TABLE {{.TablePrefix}}limit_overrides (
  subject_kind  TEXT NOT NULL CHECK (subject_kind IN ('user','org','project')),
  subject_id    TEXT NOT NULL,
  key           TEXT NOT NULL,
  value         INTEGER CHECK (value IS NULL OR value >= 0),
  updated_at    TEXT NOT NULL,
  updated_by    TEXT,
  PRIMARY KEY (subject_kind, subject_id, key)
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS {{.TablePrefix}}limit_overrides;
DROP TABLE IF EXISTS {{.TablePrefix}}plan_limits;
ALTER TABLE {{.TablePrefix}}orgs DROP COLUMN plan;

-- +goose StatementEnd
