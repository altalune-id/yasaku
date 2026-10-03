-- +goose Up
-- +goose StatementBegin

-- NOTE: a key principal carries no user, so the author column is nullable and *_by_key_id names the key instead.
ALTER TABLE {{.Schema}}.{{.TablePrefix}}transactions ALTER COLUMN created_by DROP NOT NULL;
ALTER TABLE {{.Schema}}.{{.TablePrefix}}transactions
  ADD COLUMN created_by_key_id UUID REFERENCES {{.Schema}}.{{.TablePrefix}}api_keys(id) ON DELETE RESTRICT;
ALTER TABLE {{.Schema}}.{{.TablePrefix}}transactions
  ADD CONSTRAINT {{.TablePrefix}}transactions_author_one
  CHECK ((created_by IS NULL) <> (created_by_key_id IS NULL));

ALTER TABLE {{.Schema}}.{{.TablePrefix}}period_closings ALTER COLUMN closed_by DROP NOT NULL;
ALTER TABLE {{.Schema}}.{{.TablePrefix}}period_closings
  ADD COLUMN closed_by_key_id UUID REFERENCES {{.Schema}}.{{.TablePrefix}}api_keys(id) ON DELETE RESTRICT;
ALTER TABLE {{.Schema}}.{{.TablePrefix}}period_closings
  ADD CONSTRAINT {{.TablePrefix}}period_closings_author_one
  CHECK ((closed_by IS NULL) <> (closed_by_key_id IS NULL));

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE {{.Schema}}.{{.TablePrefix}}period_closings DROP CONSTRAINT IF EXISTS {{.TablePrefix}}period_closings_author_one;
ALTER TABLE {{.Schema}}.{{.TablePrefix}}period_closings DROP COLUMN IF EXISTS closed_by_key_id;
ALTER TABLE {{.Schema}}.{{.TablePrefix}}period_closings ALTER COLUMN closed_by SET NOT NULL;
ALTER TABLE {{.Schema}}.{{.TablePrefix}}transactions DROP CONSTRAINT IF EXISTS {{.TablePrefix}}transactions_author_one;
ALTER TABLE {{.Schema}}.{{.TablePrefix}}transactions DROP COLUMN IF EXISTS created_by_key_id;
ALTER TABLE {{.Schema}}.{{.TablePrefix}}transactions ALTER COLUMN created_by SET NOT NULL;

-- +goose StatementEnd
