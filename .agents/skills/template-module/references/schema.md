# Schema: migrations, RLS, jet bindings

## Migrations

New files: `schema/migrations/postgres/NNN_<name>.sql` and
`schema/migrations/sqlite/NNN_<name>.sql`. Bump both `VERSION` files — they pin each dialect
independently and the counts differ.

Never edit an applied migration in place on a live database. Goose tracks a version number and
does **not** checksum, so an edit is a silent no-op: no error, no change. Editing in place is
only acceptable when every database will be recreated.

Every tenant-scoped table carries `org_id` and `project_id`, both `REFERENCES ... ON DELETE
CASCADE`. A join table needs its own `org_id` too — it needs its own RLS policy, and a policy
needs a column to scope on.

Prefer `ON DELETE RESTRICT` over `CASCADE` for references that represent a user's data: deleting
a category that still has posts should fail loudly as a typed error, not silently delete posts.

## RLS block

Copy the shape of `002_rls.sql` exactly. Three things must all be present or the boot audit
(`schema/rls_guard.go`) rejects the table:

```sql
{{if .RLSEnforce}}
ALTER TABLE {{.Schema}}.{{.TablePrefix}}<table> ENABLE ROW LEVEL SECURITY;
ALTER TABLE {{.Schema}}.{{.TablePrefix}}<table> FORCE ROW LEVEL SECURITY;
CREATE POLICY {{.TablePrefix}}<table>_tenant
  ON {{.Schema}}.{{.TablePrefix}}<table>
  USING (org_id = {{.Schema}}.{{.TablePrefix}}current_org_id());
{{end}}
```

Two traps:

- **The `{{if .RLSEnforce}}` wrapper is mandatory.** `002_rls.sql` defines `current_org_id()`
  inside that guard, so an unwrapped reference fails with "function does not exist" whenever
  `tenant.rlsEnforce` is false. The default is true, so `make check` will not catch this — it
  breaks only for a fork that turns RLS off.
- **`make tenant-tables` keys off the literal string** `ALTER TABLE {{.TablePrefix}}<name>
ENABLE ROW LEVEL SECURITY;`, not the `org_id` column. Write the table name without the
  `{{.TablePrefix}}` template literal and the table silently misses both the generated list and
  the boot audit.

A bare `CREATE POLICY ... USING (...)` yields `cmd = ALL`, which satisfies both the read and
write halves of the audit. Adding an explicit `WITH CHECK (true)` is a cross-tenant write hole
and the audit rejects it.

After writing the migration, run `make tenant-tables` and confirm the new tables appear in
`schema/tenant_tables_gen.go`. Never hand-edit that file.

A table with no `org_id` at all (global, like `users`, or pre-tenant, like `sessions`) gets no
policy and must stay out of the tenant list. Add a guard test pinning that, and register it in
`schema.RequiredTableSuffixes` if boot should verify it exists.

## SQLite counterpart

Same tables, `TEXT` for ids and timestamps, no `{{.Schema}}` prefix, no RLS block. Keep the
`{{.TablePrefix}}` literal on table _and_ index names. SQLite has no RLS, which is why the
adapter's explicit `org_id` predicates are load-bearing there.

## Jet bindings

Adapters use hand-written go-jet bindings, not raw SQL. Each table needs one file per dialect:

```
internal/platform/db/entity/postgres/<table>.go   New<Table>(schema, tablePrefix string)
internal/platform/db/entity/sqlite/<table>.go     New<Table>(tablePrefix string)
```

Copy `entity/postgres/orgs.go` for the shape: typed columns, an `AllColumns` list, a constructor.
Do **not** add a `MutableColumns` field — it was removed repo-wide because nothing read it.

SQLite timestamp columns are `ColumnString`, not a time type.
