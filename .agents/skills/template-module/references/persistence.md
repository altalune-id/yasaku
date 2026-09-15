# Adapters: tenant scoping, upserts, constraint translation

## Factory

```go
// NewStore dispatches to the driver-specific Store implementation.
func NewStore(cfg db.DBConfig, pool db.Pool, pc *tenant.PgConn) Store {
	if cfg.Driver == db.DriverPostgres {
		return newPostgresStore(pool, pc, cfg.Schema, cfg.TablePrefix)
	}
	return newSQLiteStore(pool.W, cfg.TablePrefix)
}
```

`MODULE_TEMPLATE.md` documents an older signature taking `*sql.DB`; follow `internal/todo/
factory.go`, which is current.

## Unit of work

`db.Pool{W, R}` wraps writer and reader handles. Two composable helpers, both enrolling via the
same `db.CurrentTx(ctx)` slot:

- `db.RunInTx(ctx, pool, fn)` — plain writer transaction, no tenant scope.
- `tenant.RunInTx(ctx, pc, tc, fn)` — sets `app.current_org_id` so RLS sees the org.

A store method enrolls in an outer transaction when one exists:

```go
func (s *postgresStore) Save(ctx context.Context, t *Todo) error {
	if tx := db.CurrentTx(ctx); tx != nil {
		return s.saveTx(ctx, tx, t)
	}
	return s.pc.BeginTenanted(ctx, func(tx *sql.Tx) error { return s.saveTx(ctx, tx, t) })
}
```

Nesting `RunInTx` returns `db.ErrNestedUnitOfWork`. A multi-statement write — a row plus its join
rows — must be one transaction on both drivers; SQLite deadlocks on `SQLITE_BUSY` otherwise.

## Tenant scoping

**Every read and write carries an explicit `org_id` predicate from `tenant.From(ctx)`**, even
methods that already take an `orgID` argument. Layer both: `posts.OrgID.EQ(orgID).AND(posts.
OrgID.EQ(tc.OrgID))` means a caller passing an org that disagrees with the request's tenant scope
matches nothing.

RLS is the backstop, not the guard. SQLite has none at all, and a `BYPASSRLS` Postgres role
slips past policies entirely — including the role the repo's own test fixtures connect as.

## The upsert guard

```go
stmt := s.table.INSERT(s.table.AllColumns).
	VALUES(...).
	ON_CONFLICT(s.table.ID).
	DO_UPDATE(postgres.SET(
		...,
	).WHERE(s.table.OrgID.EQ(postgres.UUID(tc.OrgID))))

res, err := stmt.ExecContext(ctx, tx)
// ... then:
n, _ := res.RowsAffected()
if n == 0 {
	return &NotFoundError{ID: t.ID.String()}
}
```

Without the `WHERE`, a `Save` carrying another org's row id takes the UPDATE branch and rewrites
that org's row. This was demonstrated live on both drivers: org B renamed org A's row.

Without the `RowsAffected` branch, a blocked upsert is a silent no-op that reports success.

`schema/upsert_tenant_guard_test.go` walks adapter files with go/ast and fails the build on any
`DO_UPDATE` without an `OrgID`. It covers nested packages too. If it fires, fix the code — widen
the exemption list only for a table that genuinely has no `org_id` column, and note that a stale
exemption also fails.

`ON_CONFLICT(OrgID, UserID)` — the org in the conflict _target_ — is a stronger guard than a
`WHERE` and the test accepts it.

## Constraint translation

Never leak a driver error.

**Postgres** — match on `*pgconn.PgError` SQLSTATE:

| Condition                    | Code    |
| ---------------------------- | ------- |
| unique violation             | `23505` |
| foreign key still referenced | `23503` |

**SQLite** — match on the errcode, not the message. Follow `internal/project/sqlite.go`, not
`internal/org/sqlite.go` which string-matches. The import alias matters: `sqlite` is already
go-jet in every store file, so the driver must be aliased.

```go
import (
	"github.com/go-jet/jet/v2/sqlite"
	sqlitedrv "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

var sqliteErr *sqlitedrv.Error
if errors.As(err, &sqliteErr) {
	switch sqliteErr.Code() {
	case sqlite3.SQLITE_CONSTRAINT_UNIQUE, sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY:
		return &AlreadyExistsError{Slug: c.Slug}
	// ON DELETE RESTRICT reports TRIGGER, not FOREIGNKEY — FK actions run as internal triggers;
	// only insert-side violations report FOREIGNKEY.
	case sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY, sqlite3.SQLITE_CONSTRAINT_TRIGGER:
		return &InUseError{ID: id.String()}
	}
}
```

## Timestamps

Every SQLite timestamp write goes through `sqliteent.SQLiteTime(t)`. Never
`Format(time.RFC3339Nano)` — it trims trailing zeros, so text comparison disagrees with
chronological comparison and ordering silently breaks. This applies to **bind sites too**, not
just inserts: a sweep cutoff compared against padded rows must itself be padded, or stale rows
are never swept.

## Ordering

Every `List` orders by a total order: `created_at DESC, id DESC`. `created_at` alone is not one —
`timestamptz` is microsecond precision and pgx truncates nanoseconds, so ties are reachable and
tied rows come back in arbitrary order.

If a read goes through a `SECURITY DEFINER` wrapper function, repeat the `ORDER BY` on the outer
statement and alias the set-returning function. A `SELECT` without `ORDER BY` has no guaranteed
row order regardless of what the function body does.
