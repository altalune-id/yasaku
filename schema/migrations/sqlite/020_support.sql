-- +goose Up
-- +goose StatementBegin

ALTER TABLE {{.TablePrefix}}users ADD COLUMN suspended_at TEXT;
ALTER TABLE {{.TablePrefix}}users ADD COLUMN last_sign_in_at TEXT;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE {{.TablePrefix}}users DROP COLUMN last_sign_in_at;
ALTER TABLE {{.TablePrefix}}users DROP COLUMN suspended_at;
-- +goose StatementEnd
