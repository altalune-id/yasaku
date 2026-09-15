# Wiring: fakes, boot, config, depguard

## Fake store

`internal/testutil/fakes/<name>.go` — a hand-written in-memory `Store`, mutex-guarded, with
`var _ <name>.Store = (*<Name>)(nil)` so it cannot drift from the interface.

No `sqlmock`, no `gomock`, no `testify/mock`. `testify/require` and `assert` are fine and used
throughout — `MODULE_TEMPLATE.md` says "no testify", but `CONTRIBUTING.md` and every existing
test disagree; the real rule is no mocks.

Expose function fields (`DeleteFn func(...) error`) for the one or two behaviours a test needs to
override, rather than building a configurable fake.

**A fake must not enforce what the test is proving.** If a scope test uses a fake that filters by
project, the test passes whether or not the production guard exists. Assert the fake's
non-filtering in the fixture — a raw `store.ByID` that must return the row — so the test cannot
quietly become vacuous later.

## Boot

Two files, and the split is why wiring looks missing if you only read one:

- `internal/boot/services.go` — construction, in `buildServices`, beside the existing services,
  plus fields on `Services`.
- `internal/boot/server.go` — fields on `Server` and the assignment in the return literal.

```go
posts := blog.NewService(blog.NewStore(cfg.DB, pool, pgConn), log, reporter.Unexpected)
```

**Do not register a scheduler provider unless the module has periodic work.**
`internal/boot/schedulers.go` holds a `schedulerDomains` manifest and `assertSchedulerWiring`
fails the boot if the provider list and the domain list disagree. Adding one without the other
breaks startup; adding neither is correct for most modules.

If the module does have periodic work, add `scheduler.go` with a `Scheduler` implementing
`scheduler.Provider`, register it, and add the domain name to the manifest. A `ScopeTenant` job
receives an already tenant-bound ctx — the runner fans out over tenants, so the job needs no
`org_id` handling. A `ScopeSystem` job runs once per tick with no scope. Cadence is a package
constant, not config; only the timezone is operator-tunable.

## Config

A new config key is a field on the relevant struct in `internal/platform/config/config.go` with
`yaml`, `mapstructure` and `awareness` tags. The awareness tag drives the generated examples:
`required`, `secret`, `bootstrap`, `mode:<x>`.

Env binding is reflective over the `mapstructure` tags, so a new field is picked up
automatically as `ALT_<SECTION>_<FIELD>`. Verify the generated name rather than assuming.

Cross-field rules go in `validateInvariants` as a small named function each, with an actionable
message naming the env var. **Add new validators last** in their chain — putting one first
changes which error an existing misconfiguration reports, and will break tests that assert the
old message.

Run `make config-examples` after any struct tag change; CI has a drift check.

## depguard

`domain-purity` in `.golangci.yaml` restricts imports in aggregate files. Its `files` globs are
path-shaped and `*` does not cross a `/`, so a subpackage needs its own entry:

```yaml
- "**/internal/*/<name>.go"
- "**/internal/<name>/*/{sub1,sub2,store,errors}.go"
```

Prefer a narrow alternation over `**/internal/*/*/errors.go`, which would also capture
`internal/platform/*/errors.go` and fail lint later for an unrelated-looking reason.

**Prove the glob bites.** Add a genuinely disallowed import — a third-party package, not
something from stdlib, since the allow list includes `$gostd` — and confirm `make lint` fails.
A glob that silently matches nothing is worse than no glob, because it looks like protection.
