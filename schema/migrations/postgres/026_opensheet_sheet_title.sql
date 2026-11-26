-- +goose Up
-- +goose StatementBegin

ALTER TABLE {{.Schema}}.{{.TablePrefix}}opensheet_links
  ADD COLUMN sheet_title TEXT,
  ADD COLUMN google_file_id TEXT;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE {{.Schema}}.{{.TablePrefix}}opensheet_links
  DROP COLUMN google_file_id,
  DROP COLUMN sheet_title;

-- +goose StatementEnd
