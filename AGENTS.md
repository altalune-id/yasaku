# AGENTS.md

Guide for AI coding agents (Claude Code, Codex, Cursor, …). Read before touching code.

## What yasaku is

Multitenant Go template — Templ + HTMX + Connect-RPC on one HTTP listener. Downstream
services fork it and swap the domain modules. Signatures under `authl/`, `httpclient/`,
`logger/`, `mailer/`, `mcp/`, `nanoid/`, `reqid/`, `scheduler/`, `slug/`, `telemetry/`, `worker/`
and `internal/platform/` are copied verbatim into forks — a signature change costs every
fork churn, so land tests first. `mcp/` is held to that boundary by the `mcp-purity`
depguard rule: stdlib and the MCP Go SDK only.

## Read first

| Doc                                                             | Owns                                                                                    |
| --------------------------------------------------------------- | --------------------------------------------------------------------------------------- |
| [`architecture`](docs/architecture/README.md)                   | layer map — surfaces, domain, data, and what each layer may not do                      |
| [`surfaces`](docs/surfaces/README.md)                           | the seven surfaces, mounts, credential classes, R1–R11. Read before any route           |
| [`modules`](docs/modules/README.md)                             | the 7-file domain-module shape. Reference impl: `internal/todo/`                        |
| [`platform`](docs/platform/README.md)                           | cross-cutting primitives. Reference impls: `internal/platform/session/`, `worker/`      |
| [`request scope`](docs/multitenancy/request-scope.md)           | how a request gets its tenant scope. Read before adding or moving a tenant-scoped route |
| [`scopes`](docs/scopes/README.md)                               | the scope catalog. Wire contract; additive only                                         |
| [`mcp`](docs/mcp/README.md)                                     | the MCP surface — mount, auth, adding a tool, the Apps UI                               |
| [`cli`](docs/cli/README.md)                                     | command tree, exit codes, output envelopes                                              |
| [`GLOSSARY.md`](GLOSSARY.md)                                    | canonical terms — check here before inventing a synonym                                 |
| [`README.md`](README.md) · [`CONTRIBUTING.md`](CONTRIBUTING.md) | layout, config, docker, releasing · TDD, commits, signing                               |

## Skills

`.agents/skills/` holds the real files and sets the review bar. `.claude/skills/` is
symlinks into it — edit `.agents/skills/`, never the links. Loaded automatically on task
match: `go` (idiomatic Go, through 1.25/1.26) · `go-concurrency` (goroutine ownership and
lifecycle) · `cobra-viper` · `go-release` · `go-spec-reviewer` ·
`altalune-go-convention` (module shape, tenant scoping, surfaces) ·
`altalune-go-review` (review a change: convention, reuse, extensibility, scalability) ·
`comment-discipline` (when a comment earns its place; gate is `make comment-check`) ·
`htmx-guidance`, `htmx-debugging`, `htmx-upgrade-from-htmx2`, `htmx-extension-authoring`.

## Rules that override defaults

- **Never `git commit` while implementing a plan.** The user reviews the whole tree at the end.
- **Never `--no-verify` or `--no-gpg-sign`.** Fix the hook failure instead.
- **Never the section-sign glyph (U+00A7) in output.** Use "#" or the word "Section".
- **Every HTTP route belongs to exactly one surface.** Seven exist and no eighth may be
  invented: console, control plane, data plane, ingest, dispatch, cli, mcp. Adding a route
  means choosing its surface first; [`surfaces`](docs/surfaces/README.md) fixes the names,
  mounts, credential classes and rules R1–R11.
- **No global middleware.** Each surface owns its chain (`web.SurfaceChains`). An SSR gate
  must never be reachable from a machine surface or a probe.
- **Tenant-scoped routes call `Deps.RequireProject`** (or `RequireOrg`), never their own
  `OrgScopeFor` → `ProjectScopeFor` chain. A guard test enforces it.
- **Errors are typed structs**, one per failure mode, with an `Is<FullTypeName>Error` helper
  (e.g. `IsNotFoundError`) — never `IsErr*`, never a shortcut.
- **No mocks.** Fakes are hand-written under `internal/testutil/fakes/`.
- **Never wrap jet's `NULL` singleton** — `postgres.TimestampzExp(postgres.NULL)` mutates a
  package-level var and races. Use the `Null*` helpers in
  `internal/platform/db/entity/{postgres,sqlite}`, matching the column's declared type;
  Postgres has no assignment cast, so a mistyped null fails at analyze time.
- **Nonce every script and every htmx attribute.** `<script>` takes `nonce={ d.Nonce }` — CSP
  sets a nonce-based `script-src`, so an unnonced script silently does not run. An element
  carrying htmx attributes also takes `hx-nonce={ d.Nonce }`: under an enforcing CSP the
  hx-csp extension strips htmx attributes off swapped-in fragments without it
  (`TestBase_CSPEnforced`).
- **Keys a handler resolves at runtime** need a standalone `//i18n:use <key>` comment (or
  `//i18n:use <prefix>.*`), or `make i18n-check` reads them as dead.
- **Comments — default NO comments.** Keep 1-line godoc on exported symbols,
  TODO/SECURITY/FIXME/NOTE markers, external URL references. Delete rationale, history and
  architecture prose. Enforced by `make comment-check` (`cmd/comment-lint`, CI job `comment-check`).
- **Config awareness tags.** Every `Config` field you add or change carries
  `awareness:"..."` (`required` / `bootstrap` / `secret` / `mode:<x>`; `-` opts out, and a
  nested struct inherits via `mergeAwareness`). Cloud mode ALLOWS `Genesis.Email` for
  first-admin bootstrap. NOTE: most existing keys carry no tag and nothing checks
  presence, so never infer from a neighbouring field that a tag is optional.
- **No `else` on the happy path.** Return early.
- **Cobra commands are built by factories** (`NewRootCmd`), never package-level vars. Their
  business logic lives outside `cmd/` and knows nothing about Cobra or Viper.

## Common commands

```bash
make check              # fmt + vet + templ-normalize + race tests — pre-commit gate
make test               # unit (fast)
make test-integration   # needs TEST_PG_DSN or a docker/podman socket
make generate           # regenerate templ + buf outputs
make config-examples    # regenerate .env.example + config.example.yaml
make tenant-tables      # regenerate schema/tenant_tables_gen.go
make lint               # golangci-lint (or go vet fallback)
make templ-normalize    # pin generated templ FileName paths to root-relative form
make comment-check      # CI gate: comment discipline (make comment-list = scan, no fail)
make i18n-check         # CI gate: every d.Tr key translated in every locale
make ui-vendor-check    # CI gate: committed static assets match their pinned digests
make ui-vendor          # re-vendor internal/web/static/
make mcp-ui-vendor      # re-vendor the MCP Apps assets
```

`make check` runs the race detector. Not optional: the stores build jet expressions
concurrently, and the one data race found downstream was invisible without it.

## Before finishing a task

- `make check` passes.
- Ran the integration tests, if you touched a `postgres.go` or a migration.
- Regenerated `.env.example` / `config.example.yaml` (config tags changed), `gen/` (edited a
  `.proto`), `tenant_tables_gen.go` (added a tenant-scoped table).
- Ran `bash scripts/verify-mcp-smoke.sh`, if you touched the MCP surface, a `(mcp.v1.tool)`
  annotation, or `cmd/protoc-gen-mcp`.
- Re-vendored and committed the bytes, if you changed a pinned asset.
- `make i18n-check` passes, if you touched a `.templ` string or a locale file.

## Verifying UI / server changes

- **Server smoke** — `bash scripts/verify-serve-smoke.sh` boots `yasaku serve` on ephemeral
  SQLite and a random port, curls `/healthz`, sends SIGTERM, asserts clean shutdown in 10s.
- **Live probe** — `yasaku healthz` uses the configured `http.addr`, or pass
  `--url http://host:port/healthz`. Same binary is the compose/k8s healthcheck.
- **UI** — `make dev`, then open the page. Type-checking passing is not the feature working.

## yasaku specifics

yasaku is a fork of the template. It adds a personal-finance domain on top of the template platform.

- **Modules** — `wallet`, `transaction`, `period`, `report`, `category`, `ledger`. Reference impls:
  `internal/wallet/` (flat) and `internal/transaction/` (relations and ports); see
  [`docs/modules/yasaku.md`](docs/modules/yasaku.md).
- **yasaku-only roots** — `money/`, `civil/` and `opensheet/`. They are leaf packages with their own
  depguard rules in `.golangci.yaml`.
- **blog and todo** — kept as code, mounted nowhere. Which surfaces yasaku mounts is decided in
  `internal/boot/surfaces_yasaku.go`.
- **Scopes** — `yasaku:read` and `yasaku:write` are the enforced scopes. TODO: split them into fine scopes.
- **Error codes** — yasaku's LDG, WLT, CTG, TXN and PRD codes are in
  [`docs/errors/yasaku.md`](docs/errors/yasaku.md). The registry is append-only.
- **MCP** — the 27-tool catalogue, project targeting, two-phase confirm and the UI bundle are in
  [`docs/mcp/yasaku.md`](docs/mcp/yasaku.md).
- **opensheet-api skill** — `.claude/skills/opensheet-api` is yasaku-only. Use it when calling the
  opensheet HTTP data plane.
