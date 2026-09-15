# Reviewing a module

## Checklist

- File set matches the canonical shape. No `postgres_repo.go`, no `domain/` subpackage, no
  splitting by layer.
- `Store` lives in `store.go` beside the aggregate. Verbs only, `context.Context` first.
- Every typed error has `ToAppError()` and an `Is<FullTypeName>` helper, and every code has a
  `docs/ERROR_CODES.md` row.
- Every service method opens a span.
- Every DB method carries an explicit `org_id` predicate.
- Every upsert conflict clause carries the tenant predicate and a `RowsAffected() == 0` branch.
- Every service method taking a bare id checks org **and** project.
- Every `List` orders by a total order.
- Every SQLite timestamp goes through `SQLiteTime`, bind sites included.
- Migration has `ENABLE` + `FORCE` + a policy, inside `{{if .RLSEnforce}}`, with the
  `{{.TablePrefix}}` literal; `make tenant-tables` regenerated.
- Cross-module references by UUID only.
- No `panic` in the request path. No `log.Println`. No `fmt.Print*` in domain code.
- `make check`, `make lint`, `make test-integration` green.

## Things that pass review while being wrong

These are the failures that survived a plan, a spec review, and a green test suite in this
codebase. Look for them specifically.

**A test that cannot fail.** The most common shape: a tenant-isolation test written against an
RLS-enforcing Postgres fixture. RLS refuses the write either way, so the test passes with or
without the guard it claims to cover. Ask of every security test: _in what posture is this guard
the only thing standing?_ Then check the test runs in that posture — SQLite, or a superuser
Postgres connection. Prove it by reverting the guard and watching the test fail.

**A fixture that cannot prove what it claims.** The repo's default integration fixture connects
as the container superuser, which bypasses RLS outright. A test named `..._OtherOrgIsInvisible`
on that fixture proves nothing about RLS. A real one migrates as a BYPASSRLS owner with
`RLSEnforce=true` and binds the store to a separate `NOBYPASSRLS` login role, and asserts the
policies exist so a silent regression fails at the fixture.

**A fake that enforces the thing under test.** If the in-memory store filters by project, a
project-scope test passes regardless of the production code. Assert the fake does not filter.

**A guard with no coverage.** A `WHERE` clause or a lint glob that nothing exercises. A depguard
glob that matches no files looks exactly like protection. Break it on purpose and confirm the
failure.

**A `Delete` that never loads the row.** It has no scope check at all — the store's org filter is
the only thing between a sibling project and a destructive write.

**An edited migration on a live schema.** Goose does not checksum, so `migrate up` silently does
nothing and the change appears to have been applied.

**A success message for work that did not happen.** Check that a reported side effect has a
corresponding write. A command printing `project=default` while creating no project is a real
example from this codebase.

**A comment describing a mechanism that cannot occur.** Worse than no comment, because it will be
trusted. Verify the claim before copying a rationale from a neighbouring file.

**A test that is green on macOS and red on Linux CI.** Postgres `timestamptz` is microsecond
precision. `time.Now()` on macOS is already microsecond-granular, so a nanosecond value survives
a round trip locally; on Linux it does not. An assertion like
`got.CreatedAt.Equal(want.CreatedAt)` therefore passes on every developer machine and fails in
CI. Compare against `want.CreatedAt.Truncate(time.Microsecond)`, as
`internal/org/definer_integration_test.go` does.

Related asymmetry worth knowing: SQLite stores the full nanosecond value through `SQLiteTime`,
so the same aggregate round-trips at different precision depending on the driver.

## Verifying a claim

Prefer evidence over reasoning when both are available. `EXPLAIN` the query, run the mutation,
boot the binary, open the page. Several conclusions in this codebase's history were confidently
wrong until someone ran the thing — including "the lint rule is inert" (it was not; the test used
an allowed stdlib import) and "the ORDER BY is load-bearing" (it was not; `SECURITY DEFINER`
functions are never inlined).
