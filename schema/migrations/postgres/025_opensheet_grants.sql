-- +goose Up
-- +goose StatementBegin

CREATE TABLE {{.Schema}}.{{.TablePrefix}}opensheet_grants (
  org_id             UUID NOT NULL REFERENCES {{.Schema}}.{{.TablePrefix}}orgs(id) ON DELETE CASCADE,
  project_id         UUID NOT NULL REFERENCES {{.Schema}}.{{.TablePrefix}}projects(id) ON DELETE CASCADE,
  connected_by       UUID NOT NULL,
  refresh_sealed     BYTEA NOT NULL CHECK (length(refresh_sealed) > 0),
  access_sealed      BYTEA,
  access_expires_at  TIMESTAMPTZ,
  scopes             TEXT NOT NULL,
  status             TEXT NOT NULL CHECK (status IN ('ok','reconnect_needed')),
  created_at         TIMESTAMPTZ NOT NULL,
  updated_at         TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (project_id),
  UNIQUE (project_id, org_id)
);
CREATE INDEX {{.TablePrefix}}opensheet_grants_connected_by_idx
  ON {{.Schema}}.{{.TablePrefix}}opensheet_grants (org_id, connected_by);

-- NOTE: COALESCE, because a NULL key makes a bare length() check pass.
ALTER TABLE {{.Schema}}.{{.TablePrefix}}opensheet_links
  ADD COLUMN credential TEXT NOT NULL DEFAULT 'api_key',
  ADD CONSTRAINT {{.TablePrefix}}opensheet_links_credential_check CHECK (credential IN ('api_key','grant')),
  ALTER COLUMN api_key_sealed DROP NOT NULL,
  DROP CONSTRAINT {{.TablePrefix}}opensheet_links_api_key_sealed_check,
  ADD CONSTRAINT {{.TablePrefix}}opensheet_links_key_check CHECK (credential = 'grant' OR COALESCE(length(api_key_sealed), 0) > 0);

{{if .RLSEnforce}}
ALTER TABLE {{.Schema}}.{{.TablePrefix}}opensheet_grants ENABLE ROW LEVEL SECURITY;
ALTER TABLE {{.Schema}}.{{.TablePrefix}}opensheet_grants FORCE ROW LEVEL SECURITY;
CREATE POLICY {{.TablePrefix}}opensheet_grants_tenant
  ON {{.Schema}}.{{.TablePrefix}}opensheet_grants
  USING (org_id = {{.Schema}}.{{.TablePrefix}}current_org_id());
{{end}}

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- NOTE: a grant link has no key to fall back on and cannot satisfy the restored check, so it is dropped with its sync state.
DELETE FROM {{.Schema}}.{{.TablePrefix}}opensheet_links WHERE credential = 'grant';
ALTER TABLE {{.Schema}}.{{.TablePrefix}}opensheet_links
  DROP CONSTRAINT {{.TablePrefix}}opensheet_links_key_check,
  DROP CONSTRAINT {{.TablePrefix}}opensheet_links_credential_check,
  DROP COLUMN credential,
  ALTER COLUMN api_key_sealed SET NOT NULL,
  ADD CONSTRAINT {{.TablePrefix}}opensheet_links_api_key_sealed_check CHECK (length(api_key_sealed) > 0);
DROP TABLE IF EXISTS {{.Schema}}.{{.TablePrefix}}opensheet_grants;

-- +goose StatementEnd
