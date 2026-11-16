-- +goose Up
-- +goose StatementBegin

ALTER TABLE {{.TablePrefix}}users ADD COLUMN theme TEXT NOT NULL DEFAULT '';
ALTER TABLE {{.TablePrefix}}users ADD COLUMN color_mode TEXT NOT NULL DEFAULT '';
ALTER TABLE {{.TablePrefix}}users ADD COLUMN tips_seen TEXT NOT NULL DEFAULT '[]';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE {{.TablePrefix}}users DROP COLUMN tips_seen;
ALTER TABLE {{.TablePrefix}}users DROP COLUMN color_mode;
ALTER TABLE {{.TablePrefix}}users DROP COLUMN theme;

-- +goose StatementEnd
