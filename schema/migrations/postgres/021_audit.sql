-- +goose Up
-- +goose StatementBegin

-- NOTE: no foreign key to orgs, so an org's history outlives the org row it describes.
CREATE TABLE {{.Schema}}.{{.TablePrefix}}audit_events (
  id           UUID PRIMARY KEY,
  org_id       UUID NOT NULL,
  project_id   UUID,
  actor_kind   TEXT NOT NULL CHECK (actor_kind IN ('user','staff','api_key','system')),
  actor_id     UUID,
  actor_email  TEXT NOT NULL DEFAULT '',
  action       TEXT NOT NULL,
  target_kind  TEXT NOT NULL DEFAULT '',
  target_id    TEXT NOT NULL DEFAULT '',
  reason       TEXT NOT NULL DEFAULT '',
  request_id   TEXT NOT NULL DEFAULT '',
  detail       JSONB NOT NULL DEFAULT '{}',
  occurred_at  TIMESTAMPTZ NOT NULL
);
CREATE INDEX {{.TablePrefix}}audit_events_org_time_idx
  ON {{.Schema}}.{{.TablePrefix}}audit_events (org_id, occurred_at DESC);

-- SECURITY: refuses UPDATE and DELETE whatever the row level security setting, so the log stays append-only with RLS off.
CREATE OR REPLACE FUNCTION {{.Schema}}.{{.TablePrefix}}audit_events_append_only()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = {{.Schema}}, pg_catalog, pg_temp
AS $fn$
BEGIN
  RAISE EXCEPTION 'audit_events is append-only';
END;
$fn$;

CREATE TRIGGER {{.TablePrefix}}audit_events_append_only
  BEFORE UPDATE OR DELETE ON {{.Schema}}.{{.TablePrefix}}audit_events
  FOR EACH ROW EXECUTE FUNCTION {{.Schema}}.{{.TablePrefix}}audit_events_append_only();

-- SECURITY: answers from any tenant scope whether a user holds the built-in owner or admin role in the system org, in one query on the caller's connection.
CREATE OR REPLACE FUNCTION {{.Schema}}.{{.TablePrefix}}is_staff(p_user uuid)
RETURNS boolean
LANGUAGE sql STABLE
SECURITY DEFINER
SET search_path = {{.Schema}}, pg_catalog, pg_temp
AS $fn$
  SELECT EXISTS (
    SELECT 1 FROM {{.Schema}}.{{.TablePrefix}}memberships m
    WHERE m.user_id = p_user
      AND m.role IN ('owner', 'admin')
      AND m.org_id = (SELECT o.id FROM {{.Schema}}.{{.TablePrefix}}orgs o WHERE o.system ORDER BY o.created_at ASC, o.id ASC LIMIT 1)
  );
$fn$;

REVOKE EXECUTE ON FUNCTION {{.Schema}}.{{.TablePrefix}}is_staff(uuid) FROM PUBLIC;

{{if .RLSEnforce}}
ALTER TABLE {{.Schema}}.{{.TablePrefix}}audit_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE {{.Schema}}.{{.TablePrefix}}audit_events FORCE ROW LEVEL SECURITY;
-- SECURITY: only SELECT and INSERT policies exist, so UPDATE and DELETE match no row: the log is append-only.
CREATE POLICY {{.TablePrefix}}audit_events_tenant_read
  ON {{.Schema}}.{{.TablePrefix}}audit_events FOR SELECT
  USING (org_id = {{.Schema}}.{{.TablePrefix}}current_org_id());
CREATE POLICY {{.TablePrefix}}audit_events_tenant_insert
  ON {{.Schema}}.{{.TablePrefix}}audit_events FOR INSERT
  WITH CHECK (org_id = {{.Schema}}.{{.TablePrefix}}current_org_id());
{{end}}

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP FUNCTION IF EXISTS {{.Schema}}.{{.TablePrefix}}is_staff(uuid);
DROP TABLE IF EXISTS {{.Schema}}.{{.TablePrefix}}audit_events;
DROP FUNCTION IF EXISTS {{.Schema}}.{{.TablePrefix}}audit_events_append_only();
-- +goose StatementEnd
