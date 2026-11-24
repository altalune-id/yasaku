-- +goose Up
-- +goose StatementBegin

ALTER TABLE {{.Schema}}.{{.TablePrefix}}users ADD COLUMN suspended_at TIMESTAMPTZ;
ALTER TABLE {{.Schema}}.{{.TablePrefix}}users ADD COLUMN last_sign_in_at TIMESTAMPTZ;

-- SECURITY: lists every org for the staff console across tenants: identity and counts only, never ledger rows.
CREATE OR REPLACE FUNCTION {{.Schema}}.{{.TablePrefix}}admin_list_orgs()
RETURNS TABLE (id uuid, slug text, name text, plan text, created_at timestamptz, member_count bigint, project_count bigint, last_active timestamptz)
LANGUAGE sql STABLE
SECURITY DEFINER
SET search_path = {{.Schema}}, pg_catalog, pg_temp
AS $fn$
  SELECT o.id, o.slug, o.name, o.plan, o.created_at,
         (SELECT count(*) FROM {{.Schema}}.{{.TablePrefix}}memberships m WHERE m.org_id = o.id),
         (SELECT count(*) FROM {{.Schema}}.{{.TablePrefix}}projects p WHERE p.org_id = o.id),
         (SELECT max(u.last_sign_in_at) FROM {{.Schema}}.{{.TablePrefix}}memberships m
            JOIN {{.Schema}}.{{.TablePrefix}}users u ON u.id = m.user_id WHERE m.org_id = o.id)
  FROM {{.Schema}}.{{.TablePrefix}}orgs o
  ORDER BY o.created_at DESC, o.id ASC;
$fn$;

-- SECURITY: lists every user for the staff console across tenants: identity, sign-in and suspension only.
CREATE OR REPLACE FUNCTION {{.Schema}}.{{.TablePrefix}}admin_list_users()
RETURNS TABLE (id uuid, email text, name text, created_at timestamptz, last_sign_in_at timestamptz, suspended_at timestamptz, org_count bigint)
LANGUAGE sql STABLE
SECURITY DEFINER
SET search_path = {{.Schema}}, pg_catalog, pg_temp
AS $fn$
  SELECT u.id, u.email, u.name, u.created_at, u.last_sign_in_at, u.suspended_at,
         (SELECT count(*) FROM {{.Schema}}.{{.TablePrefix}}memberships m WHERE m.user_id = u.id)
  FROM {{.Schema}}.{{.TablePrefix}}users u
  ORDER BY u.created_at DESC, u.id ASC;
$fn$;

REVOKE EXECUTE ON FUNCTION {{.Schema}}.{{.TablePrefix}}admin_list_orgs() FROM PUBLIC;
REVOKE EXECUTE ON FUNCTION {{.Schema}}.{{.TablePrefix}}admin_list_users() FROM PUBLIC;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP FUNCTION IF EXISTS {{.Schema}}.{{.TablePrefix}}admin_list_users();
DROP FUNCTION IF EXISTS {{.Schema}}.{{.TablePrefix}}admin_list_orgs();
ALTER TABLE {{.Schema}}.{{.TablePrefix}}users DROP COLUMN IF EXISTS last_sign_in_at;
ALTER TABLE {{.Schema}}.{{.TablePrefix}}users DROP COLUMN IF EXISTS suspended_at;
-- +goose StatementEnd
