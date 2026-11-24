-- +goose Up
-- +goose StatementBegin

CREATE TABLE {{.TablePrefix}}audit_events (
  id           TEXT PRIMARY KEY,
  org_id       TEXT NOT NULL,
  project_id   TEXT,
  actor_kind   TEXT NOT NULL CHECK (actor_kind IN ('user','staff','api_key','system')),
  actor_id     TEXT,
  actor_email  TEXT NOT NULL DEFAULT '',
  action       TEXT NOT NULL,
  target_kind  TEXT NOT NULL DEFAULT '',
  target_id    TEXT NOT NULL DEFAULT '',
  reason       TEXT NOT NULL DEFAULT '',
  request_id   TEXT NOT NULL DEFAULT '',
  detail       TEXT NOT NULL DEFAULT '{}',
  occurred_at  TEXT NOT NULL
);
CREATE INDEX {{.TablePrefix}}audit_events_org_time_idx ON {{.TablePrefix}}audit_events (org_id, occurred_at DESC);

CREATE TRIGGER {{.TablePrefix}}audit_events_no_update BEFORE UPDATE ON {{.TablePrefix}}audit_events
BEGIN
  SELECT RAISE(ABORT, 'audit_events is append-only');
END;

CREATE TRIGGER {{.TablePrefix}}audit_events_no_delete BEFORE DELETE ON {{.TablePrefix}}audit_events
BEGIN
  SELECT RAISE(ABORT, 'audit_events is append-only');
END;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS {{.TablePrefix}}audit_events;
-- +goose StatementEnd
