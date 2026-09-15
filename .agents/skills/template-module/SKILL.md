---
name: template-module
description: Add or review a domain module (bounded context) in this Go multitenant template (yasaku) or a downstream fork of it — the DDD package shape, tenant scoping and RLS, guarded upserts, typed errors and error codes, migrations, boot wiring, and the web/RPC surfaces. Use this whenever adding a new domain, aggregate, subdomain, Store, migration or error code to a codebase built on this template, or when reviewing such a change. Also use it when the task mentions tenant isolation, org scoping, row level security, ON CONFLICT, or "how do I add a module here".
license: Proprietary
metadata:
  reference-impl: internal/blog (relations), internal/todo (flat)
---

# Adding a domain module

A module is one bounded context under `internal/<name>/`. Copy `internal/todo/` for a flat
domain, `internal/blog/` for one with relations and subdomains.

## Order of work

Do these in order — each step's verification depends on the previous one.

1. **Schema** — migrations + RLS + jet bindings → `references/schema.md`
2. **Error codes** — `apperror/codes.go` + `docs/ERROR_CODES.md` → `references/errors.md`
3. **Domain** — aggregate, `Store`, typed errors, service → `references/domain.md`
4. **Adapters** — `postgres.go` / `sqlite.go` → `references/persistence.md`
5. **Wiring** — fake, boot, depguard → `references/wiring.md`
6. **Surfaces** — web and/or RPC → `references/surfaces.md`
7. **Verify** — `scripts/verify.sh`

`scripts/scaffold.sh <name>` copies `internal/todo/` and renames the package, type and
identifiers. It is a starting point, not a finished module.

## Rules that are expensive to get wrong

These caused real defects in this codebase. The rest of the conventions are in the references.

**Every query carries an explicit `org_id` predicate from `tenant.From(ctx)`.** Do not rely on
RLS alone: SQLite has no RLS at all, and a `BYPASSRLS` role slips past Postgres. RLS is the
backstop, not the guard.

**An upsert's conflict clause carries the tenant predicate**, plus a `RowsAffected() == 0`
branch returning `&NotFoundError{}`. Without it, a `Save` carrying another org's row id takes
the UPDATE branch and rewrites that row. `schema/upsert_tenant_guard_test.go` fails the build
on any unguarded `DO_UPDATE`; fix the code rather than widening its exemption list.

**A service method taking a bare id checks org _and_ project.** The store filters by org, not
project, so without this a sibling project's row is reachable — and a `Delete` that never reads
the row first will happily destroy it. See `category.Service.ByID` for the shape.

**A security guard's test must run where the guard is the only protection.** A hijack test on an
RLS-enforcing Postgres fixture cannot detect its own guard's removal — RLS refuses the write
either way, so it passes with or without the code under test. Put those tests on SQLite or a
superuser fixture, and prove each one by reverting the guard and watching it fail.

**Typed errors only**, one struct per failure mode, helper named `Is<FullTypeName>` — never
`IsErr*`. Error codes are append-only and verified against `docs/ERROR_CODES.md` in both
directions.

## Conventions in brief

- One package per bounded context; the package name is the domain term.
- Split into `internal/<name>/<sub>/` only when the subdomain has its own aggregate root, its own
  `Store` **and** its own table. Otherwise add files. Cross-reference by UUID, never by pointer.
- `Store` verbs: `Save`, `ByID`, `List`, `Delete`. Never `Get*`/`Find*`. `context.Context` first.
- Aggregates hold invariants; services orchestrate; adapters translate driver errors.
- `Service` takes `store, log, unexpected` first; module-specific deps after.
- Every service method opens a span: `tracer.Start(ctx, "<name>.<Method>")`.
- Every `List` orders by a total order — `created_at DESC, id DESC`. `created_at` alone is not.
- Comments: default none. 1-line godoc on exported symbols, plus `SECURITY:`/`NOTE:`/`TODO:`
  markers. No rationale prose — that belongs in the commit message.

## Reviewing a module

Read `references/review.md`. It is the checklist plus the specific things that pass review while
being wrong — unguarded upserts, tests that pass vacuously, and fixtures that cannot prove what
they claim.

## Verifying

`scripts/verify.sh` runs the gates in dependency order and explains each failure. Run it before
claiming a module is done.

Integration tests need Postgres. Set `TEST_PG_DSN` at a throwaway database and use `-p 1` —
without it, packages race on `CREATE/DROP ROLE` and cleanup fails. Without `TEST_PG_DSN` each
`pgtest.New` starts its own container and the suite takes ~25 minutes instead of ~2.

Type checking passing is not evidence a surface works. Open the page.
