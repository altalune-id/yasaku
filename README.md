# yasaku

Personal cashflow tracking, multitenant. You keep wallets, log what goes in and
out, and close the books ("tutup buku") on your own payday cycle rather than on
the calendar month — then read the period back as a report. Everything lives
inside a project, so one account can keep several separate ledgers. Four
presentation layers sit on the same services: Templ + HTMX SSR, Connect-RPC,
MCP, and the scheduler, all on one HTTP listener.

Built on the altalune multitenant Go template. Module path:
`altalune.id/yasaku`. Binary: `yasaku`.

## Quick start

Local binary (SQLite, single genesis admin):

```bash
make build
YASAKU_GENESIS_EMAIL=admin@local YASAKU_GENESIS_PASSWORD=change-me ./bin/yasaku serve
# open http://127.0.0.1:5150/login
```

Full stack (Postgres + Mailpit + yasaku) via `compose.yaml`:

```bash
make compose-up
# yasaku:  http://127.0.0.1:5150/login
# mailpit:  http://127.0.0.1:8025    (every outbound email lands here)
```

Cloud config (Postgres + OIDC):

```bash
cp config.example.yaml config.yaml    # edit
make build
./bin/yasaku -c config.yaml serve
```

`make help` lists every target. `make check` is the pre-commit gate; releases are cut with
GoReleaser from `.goreleaser.yaml` — see [`CONTRIBUTING.md`](CONTRIBUTING.md#releasing).

## Layout

```
yasaku/
├── api/                # buf-managed proto sources, incl. the (mcp.v1.tool) annotation
├── authl/              # RFC 8252 OIDC PKCE loopback (exported)
├── cmd/yasaku/        # main package
├── cmd/protoc-gen-mcp/ # protoc plugin: annotated RPCs → MCP tool registrations
├── cmd/…               # build-time tools: comment-lint, i18n-lint, gen-config-example, gen-tenant-tables
├── docs/               # architecture, surfaces, scopes, MCP, configuration, deployment, CLI contract, templates
├── gen/                # generated proto (do not edit)
├── internal/
│   ├── apperror/       # stable error codes + Reporter fan-out
│   ├── auth/           # local + OIDC login orchestration
│   ├── boot/           # composition root
│   ├── cli/            # cobra command tree — the cli surface (S6)
│   ├── apikey/, blog/, invite/, onboard/, org/, project/, todo/, user/   # domain modules
│   ├── category/, ledger/, period/, report/, transaction/, wallet/   # yasaku domain modules
│   ├── legal/          # embedded Terms of Service + Privacy Policy markdown
│   ├── controlplane/   # Connect-RPC surface (S2), mounted at /api/
│   ├── dataplane/      # REST surface for integrators (S3), mounted at /api/v1/
│   ├── ingest/         # inbound provider webhooks (S4), mounted at /hooks/
│   ├── mcp/            # MCP surface (S7): auth, scope catalog, metadata, the Apps UI bundle
│   ├── i18n/           # locale bundles + the SSR locale middleware
│   ├── password/       # argon2id hashing
│   ├── platform/       # authn, capabilities, config, db, notify, outbox, sealer, session, surfaces, tenant, tokens
│   ├── testutil/       # hand-written fakes and test harnesses (no mocks)
│   └── web/            # console surface (S1): templ + htmx handlers, icons, static assets
├── logger/, mailer/, nanoid/, reqid/, scheduler/, telemetry/, worker/   # exported roots
├── httpclient/, mcp/   # exported roots: SSRF-guarded HTTP client, MCP server runtime
├── civil/, money/, opensheet/   # yasaku-only exported roots: calendar date, int64 minor-unit amounts, opensheet API client
├── schema/             # embedded goose migrations + RLS guard
├── scripts/            # vendoring, templ normalization, DB provisioning, smoke checks
└── version/            # build-time version info
```

## Exported packages

Safe for external Go projects to import:

| Package      | Purpose                                                                      |
| ------------ | ---------------------------------------------------------------------------- |
| `authl`      | OIDC client + PKCE loopback                                                  |
| `reqid`      | UUIDv7 request-ID propagation                                                |
| `nanoid`     | 21-char nanoid generator                                                     |
| `worker`     | Supervisor + Worker interface + HTTP/Func adapters                           |
| `scheduler`  | Cron/interval job runner — system and per-tenant scope, leader election      |
| `logger`     | `slog.Handler` — auto-attaches request_id/trace_id, key redaction            |
| `telemetry`  | OTel tracer + meter + Prometheus reader                                      |
| `mailer`     | Transactional mail — `console`, `smtp`, `resend` drivers                     |
| `httpclient` | Retrying HTTP client whose dialer refuses private and loopback addresses     |
| `mcp`        | Scope-checked MCP tool registry over the MCP Go SDK's streamable-HTTP server |
| `money`      | `int64` minor-unit amounts; arithmetic panics on currency mismatch           |
| `civil`      | Timezone-free calendar date with `driver.Valuer` / `sql.Scanner`             |
| `opensheet`  | Client for the opensheet API — paginated `iter.Seq2`, typed errors           |

Pre-1.0.0: minor releases may break; pin exact versions. Post-1.0.0:
exported surface is frozen, additive changes only. Everything under
`internal/` is private.

## Sign-up and invitation policies

| Mode                | Sign-in path                                  | Behavior                                                                                     |
| ------------------- | --------------------------------------------- | -------------------------------------------------------------------------------------------- |
| Selfhosted, no OIDC | Local `/login` (genesis + password-set users) | Works. No invites, no T&C step.                                                              |
| Selfhosted, no OIDC | `POST /orgs/{slug}/invites`                   | Blocked with 409; invites banner shown, form hidden.                                         |
| Selfhosted + OIDC   | Uninvited OIDC sign-in                        | Rejected before persistence; renders a 403 "not invited" page.                               |
| Selfhosted + OIDC   | Invited OIDC sign-in                          | User created, membership from invite, `/welcome` (T&C), then dashboard.                      |
| Cloud (OIDC forced) | Invited OIDC sign-in                          | Same as selfhosted + OIDC invited.                                                           |
| Cloud (OIDC forced) | Uninvited OIDC sign-in                        | User created, no silent org, redirect to `/signup/complete` to name the org + first project. |

Invite issuance requires `mode=cloud` or `oidc.issuer` set. `/signup/complete` runs only in cloud mode; the T&C checkbox appears when `compliance.requireAcceptance=true`.

## Documentation

| Doc                                                   | For                                                  |
| ----------------------------------------------------- | ---------------------------------------------------- |
| [`config`](docs/config/README.md)                     | precedence, awareness tags, modes, first-boot        |
| [`deployment`](docs/deployment/README.md)             | docker, DB roles, RLS, replica, observability, OIDC  |
| [`cli`](docs/cli/README.md)                           | stable command tree, exit codes, output envelopes    |
| [`architecture`](docs/architecture/README.md)         | the layer map: surfaces, domain, data                |
| [`surfaces`](docs/surfaces/README.md)                 | the seven surfaces, their mounts, credentials, rules |
| [`multitenancy`](docs/multitenancy/README.md)         | what a tenant is, how the schema and RLS enforce it  |
| [`request scope`](docs/multitenancy/request-scope.md) | how a request gets its tenant scope                  |
| [`scopes`](docs/scopes/README.md)                     | the scope catalog integrators mint keys against      |
| [`mcp`](docs/mcp/README.md)                           | the MCP surface: mount, auth, tools, UI, testing     |
| [`error codes`](docs/errors/README.md)                | stable error codes and their envelopes               |
| [`modules`](docs/modules/README.md)                   | adding a domain module                               |
| [`platform`](docs/platform/README.md)                 | adding a platform primitive                          |
| [`CONTRIBUTING.md`](CONTRIBUTING.md)                  | workflow, testing, release                           |
| [`GLOSSARY.md`](GLOSSARY.md)                          | one concept, one name — canonical terminology        |
| [`AGENTS.md`](AGENTS.md)                              | rules for AI coding agents                           |

## License

Apache 2.0 — see [`LICENSE`](LICENSE).
