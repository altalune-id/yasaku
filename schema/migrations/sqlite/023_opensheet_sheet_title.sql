-- +goose Up
-- +goose StatementBegin

ALTER TABLE {{.TablePrefix}}opensheet_links ADD COLUMN sheet_title TEXT;
ALTER TABLE {{.TablePrefix}}opensheet_links ADD COLUMN google_file_id TEXT;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE {{.TablePrefix}}opensheet_links DROP COLUMN google_file_id;
ALTER TABLE {{.TablePrefix}}opensheet_links DROP COLUMN sheet_title;

-- +goose StatementEnd
