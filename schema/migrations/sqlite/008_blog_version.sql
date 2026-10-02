-- +goose Up
-- +goose StatementBegin

ALTER TABLE {{.TablePrefix}}blog_posts ADD COLUMN version INTEGER NOT NULL DEFAULT 1;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE {{.TablePrefix}}blog_posts DROP COLUMN version;

-- +goose StatementEnd
