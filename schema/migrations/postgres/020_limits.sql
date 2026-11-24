-- +goose Up
-- +goose StatementBegin

ALTER TABLE {{.Schema}}.{{.TablePrefix}}orgs ADD COLUMN plan TEXT NOT NULL DEFAULT 'first';

CREATE TABLE {{.Schema}}.{{.TablePrefix}}plan_limits (
  plan        TEXT NOT NULL,
  key         TEXT NOT NULL,
  value       BIGINT CHECK (value IS NULL OR value >= 0),
  updated_at  TIMESTAMPTZ NOT NULL,
  updated_by  UUID,
  PRIMARY KEY (plan, key)
);

CREATE TABLE {{.Schema}}.{{.TablePrefix}}limit_overrides (
  subject_kind  TEXT NOT NULL CHECK (subject_kind IN ('user','org','project')),
  subject_id    UUID NOT NULL,
  key           TEXT NOT NULL,
  value         BIGINT CHECK (value IS NULL OR value >= 0),
  updated_at    TIMESTAMPTZ NOT NULL,
  updated_by    UUID,
  PRIMARY KEY (subject_kind, subject_id, key)
);

-- SECURITY: counts orgs a user created across every tenant, so the org-creation limit holds before any scope exists.
CREATE OR REPLACE FUNCTION {{.Schema}}.{{.TablePrefix}}count_orgs_created_by(p_user uuid)
RETURNS bigint
LANGUAGE sql STABLE
SECURITY DEFINER
SET search_path = {{.Schema}}, pg_catalog, pg_temp
AS $$
  SELECT count(*) FROM {{.Schema}}.{{.TablePrefix}}orgs o WHERE o.created_by = p_user AND NOT o.system;
$$;

REVOKE EXECUTE ON FUNCTION {{.Schema}}.{{.TablePrefix}}count_orgs_created_by(uuid) FROM PUBLIC;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP FUNCTION IF EXISTS {{.Schema}}.{{.TablePrefix}}count_orgs_created_by(uuid);
DROP TABLE IF EXISTS {{.Schema}}.{{.TablePrefix}}limit_overrides;
DROP TABLE IF EXISTS {{.Schema}}.{{.TablePrefix}}plan_limits;
ALTER TABLE {{.Schema}}.{{.TablePrefix}}orgs DROP COLUMN IF EXISTS plan;

-- +goose StatementEnd
