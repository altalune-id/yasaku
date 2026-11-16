-- +goose Up
-- +goose StatementBegin

ALTER TABLE {{.Schema}}.{{.TablePrefix}}users ADD COLUMN theme TEXT NOT NULL DEFAULT '';
ALTER TABLE {{.Schema}}.{{.TablePrefix}}users ADD COLUMN color_mode TEXT NOT NULL DEFAULT '';
ALTER TABLE {{.Schema}}.{{.TablePrefix}}users ADD COLUMN tips_seen TEXT NOT NULL DEFAULT '[]';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE {{.Schema}}.{{.TablePrefix}}users DROP COLUMN IF EXISTS tips_seen;
ALTER TABLE {{.Schema}}.{{.TablePrefix}}users DROP COLUMN IF EXISTS color_mode;
ALTER TABLE {{.Schema}}.{{.TablePrefix}}users DROP COLUMN IF EXISTS theme;

-- +goose StatementEnd
