-- +goose Up
-- +goose StatementBegin

-- NOTE: amounts and balances now go out as numbers, so every mirrored row is marked dirty once, given-up rows included, and the existing sync rewrites it.
UPDATE {{.Schema}}.{{.TablePrefix}}opensheet_sync_state SET synced_version = 0, attempts = 0, retry_after = NULL;

-- +goose StatementEnd

-- +goose Down
