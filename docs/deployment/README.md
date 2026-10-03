# Deployment

Key names, defaults and awareness tags: [`config`](../config/README.md). Command tree and
exit codes: [`cli`](../cli/README.md). Task recipes: [`howto/`](../howto/README.md).

## Docker

```bash
# single container, SQLite in the image
make docker
docker run --rm -p 5150:5150 \
  -e YASAKU_DB_DRIVER=sqlite -e YASAKU_DB_DSN=/data/yasaku.db \
  -e YASAKU_GENESIS_EMAIL=admin@local -e YASAKU_GENESIS_PASSWORD=change-me \
  -v yasaku-data:/data yasaku:dev
```

| Tag                                              | Source                               | Built by                    |
| ------------------------------------------------ | ------------------------------------ | --------------------------- |
| `ghcr.io/<owner>/yasaku:edge`                    | latest push to `main`                | `.github/workflows/dev.yml` |
| `ghcr.io/<owner>/yasaku:<short-sha>`             | that push, pinned                    | same                        |
| `ghcr.io/<owner>/yasaku:<version>`               | tagged release (`v0.1.0` → `:0.1.0`) | GoReleaser, `release.yml`   |
| `ghcr.io/<owner>/yasaku:latest`                  | most recent tagged release           | same                        |
| `ghcr.io/<owner>/yasaku:<version>-{amd64,arm64}` | per-arch inputs to the manifest      | same                        |

The image is distroless `nonroot`, `ENTRYPOINT ["/yasaku"]`, `CMD ["serve"]`, exposing 5150 — so any
subcommand runs as `docker run --rm yasaku:dev migrate status`. Releases are cosign-signed and carry
an `attest-build-provenance` attestation. `<owner>` comes from `GHCR_OWNER` on release and
`github.repository_owner` on `main`. Base images are digest-pinned in `Dockerfile` — refresh with
`docker manifest inspect <ref>` and update the two `ARG` lines.

## Local dev stack (compose)

`compose.yaml` starts Postgres 17 + [Mailpit](https://mailpit.axllent.org/) + NATS (JetStream,
token `yasaku-dev`) + yasaku, under `docker compose` or `podman-compose`. yasaku is at
`http://127.0.0.1:5150/login`; every outbound email lands in Mailpit at `http://127.0.0.1:8025`.

```bash
make compose-up          # build + start everything
make compose-logs
make compose-down        # stop, keep volumes
make compose-nuke        # stop + wipe docker/data/pg
```

Postgres data lives at `./docker/data/pg` and NATS data at `./docker/data/nats` (bind-mounted,
`.gitignore`d). The stack runs `selfhosted` with `YASAKU_DB_ALLOW_BYPASS_RLS=true` (RLS off),
Mailpit's open SMTP and the queue on; production mail goes under `mail.smtp.*` (or `mail.resend.*`
with `mail.driver=resend`), and the role graph below.

## Postgres

**Dev**: point `YASAKU_DB_DSN` at any role (superuser is fine), set `YASAKU_DB_ALLOW_BYPASS_RLS=true`; RLS is off.

**Production**: `scripts/db/provision.sh` creates a six-role graph — `yasaku_owner`
(`NOLOGIN CREATEROLE INHERIT BYPASSRLS`, owns every object), `yasaku_migrator` and
`yasaku_service` (LOGIN, neither holds `BYPASSRLS`), and `yasaku_editor` / `_reader` / `_ops`
(NOLOGIN human groups). No role is both LOGIN and `BYPASSRLS`. The role table, the
`SECURITY DEFINER` wrappers for cross-tenant reads and why `GRANT yasaku_owner TO x` does not confer
`BYPASSRLS`: [`multitenancy`](../multitenancy/README.md#postgres-roles). Day-2 operator procedures, provider
notes and the post-migration `verify.template.sql`: [`scripts/db/README.md`](../../scripts/db/README.md).

Provision idempotently (interactive; prompts for admin URL, DB name, passwords), then point the app
at the two LOGIN roles:

```bash
APP=yasaku DB_NAME=yasaku scripts/db/provision.sh

YASAKU_DB_DSN=postgres://yasaku_service:<svc-pw>@host:5432/yasaku?sslmode=require
YASAKU_DB_MIGRATOR_DSN=postgres://yasaku_migrator:<mig-pw>@host:5432/yasaku?sslmode=require
YASAKU_DB_MIGRATOR_ROLE=yasaku_owner
YASAKU_DB_ALLOW_BYPASS_RLS=false
```

- `db.migrator.role` is the sole source of the migration role, issued once per connection rather
  than inside the migration SQL; `db.role` applies only to runtime connections.
- `005_definer_functions.sql` refuses to apply when the migration role lacks `BYPASSRLS`, and names
  the `ALTER ROLE` that fixes it.
- Boot fails if the runtime role has `BYPASSRLS` and `db.allowBypassRLS` is `false`
  (`schema.RLSGuard`, `ErrRLSBypass`), and audits every tenant table for RLS, FORCE and a scoped
  policy.
- `mode=cloud` with `db.autoMigrate` and RLS enforced requires `db.migrator.dsn` — boot rejects the
  combination otherwise.

## Reader replica

`db.Pool{W, R}` wraps writer + reader. SQLite always aliases `R` to `W`. For Postgres,
`YASAKU_DB_READER_DSN` routes non-tenant reads (`user`, `onboard`) to a replica; empty aliases to `W`.
Tenant-scoped reads run on `W` — `tenant.PgConn.BeginTenanted` needs `set_config` inside the
transaction, which a replica cannot serve. Unit-of-work primitives:
[`multitenancy`](../multitenancy/README.md#unit-of-work); module authors:
[`modules`](../modules/README.md#3-tenant-scoping).

## Health probes

`/healthz`, `/readyz` and `/robots.txt` are mounted at the outer mux root on the `Probes` chain —
NOT under `http.basePath`. Deliberately: orchestrator probes (compose, kubelet, LB target groups)
reach yasaku on its listen port directly, so their config survives a remount; and public proxies
typically route only `example.com/<basePath>/*`, keeping `/healthz` off the public surface.

- `/healthz` — the process is serving. Never touches the DB.
- `/readyz` — every DB handle passed the most recent probe (200), else **503**. Reads the snapshot.

Boot probes once synchronously before the listener accepts traffic, so `/readyz` is accurate from
the first request — no unready window on rollout. `db-health` then refreshes the snapshot every
`db.health.interval`, so readiness lags a DB outage by up to one interval. It is a standalone `Worker`
on the HTTP listener's `Supervisor`, not a scheduler job, so it runs in **every** replica regardless
of `scheduler.enabled` or `serve --no-scheduler`. Each replica probes its own pool; the snapshot is
never shared. A probe failure is logged and the worker keeps ticking — it never reaches the
notification sinks and never fails the process.

**`yasaku healthz`** is the compose and k8s healthcheck: a self-contained probe that needs no
`curl`, which the distroless image does not have.

- Default target is `http://127.0.0.1:<port from http.addr>/healthz`.
- `--url` (or `YASAKU_URL`) moves it; a saved CLI profile deliberately does not.
- `--timeout` defaults to `3s`. Exit is non-zero when the probe fails; `--output json` prints
  `{url, status, ok, took, error}`.
- Public status page? Route it explicitly in the proxy, e.g. nginx
  `location = /yasaku/healthz { proxy_pass http://yasaku:5150/healthz; }`.
- Pre-deploy smoke test: `bash scripts/verify-serve-smoke.sh` boots `serve` on ephemeral SQLite,
  curls `/healthz`, sends SIGTERM and asserts clean shutdown.

## Scheduler and queue

Scheduler singletons, pool sizing, NATS on Railway, the `serve` deployment shapes and what
multiple replicas need: [`workers`](workers.md).

## MCP

`/mcp` (S7) is off by default. The env block, the enforced enable order (`tokens.issuer` →
`http.baseURL` → `mcp.enabled` → `mcp.challengeToken`, each omission a boot error), the audience
rules — default `baseURL + basePath + /mcp`, widened only by `mcp.audienceOverride` — and
`bash scripts/verify-mcp-smoke.sh`: [`mcp`](../mcp/README.md#setup).

Deployment-side: the proxy must pass `/.well-known/oauth-protected-resource<basePath>/mcp` and the
challenge path through — both are unauthenticated and both are how a host discovers the endpoint. A proxy
terminating on a different public URL needs `YASAKU_MCP_AUDIENCE` plus `YASAKU_MCP_AUDIENCE_OVERRIDE=true`.

## Observability

| Concern | Keys                                                                                        |
| ------- | ------------------------------------------------------------------------------------------- |
| Traces  | `telemetry.tracing.enabled` + `telemetry.otlp.endpoint` (`protocol`, `insecure`, `headers`) |
| Metrics | `telemetry.metrics.enabled`, `telemetry.metrics.exportInterval`                             |
| Scrape  | `telemetry.metrics.prometheus.enabled` / `.addr` (`:9091`) / `.path` (`/metrics`)           |
| Logs    | `log.level`, `log.format`, `log.addSource`, `log.redactPatterns`                            |
| Alerts  | `observability.reporter.minSeverity` (`error`), `observability.reporter.sinks`              |

- Prometheus listens on **its own** address, not the app listener — keep `:9091` off the public
  network rather than reverse-proxying it.
- HTTP, Connect, worker and DB spans all propagate via `context.Context`. Every request log carries
  `request_id` and `trace_id`.
- Unhandled errors fan out via `apperror.Reporter` to `internal/platform/notify` sinks: `stdout`,
  `slack`, `discord`, `googlechat`, `email`. Each entry in `observability.reporter.sinks` is
  `{kind, webhookUrl, to, from, subject}`.
- SECURITY: these sinks reach **operators**. A tenant's endpoint belongs on the dispatch surface,
  never here — see [`surfaces`](../surfaces/README.md) S5.

## API surface

Connect-RPC mounts under `basePath + /api` (`api.enabled`). `todo.v1.TodoService`,
`blog.v1.BlogService`, `apikey.v1.APIKeyService` and `auth.v1.AuthService` ship in the scaffold.
OpenAPI 3.1 is embedded at build time and served at `basePath + /api/openapi.{yaml,json}`, with a
rendered view at `basePath + /api/docs`. All three sit behind `api.openapi.requireBasicAuth` (default
`true`) using `api.openapi.basicAuthUser` / `basicAuthPassword`; `api.openapi.enabled: false` 404s
them.

## OIDC

1. In the authorization server's console, create an OAuth client — `confidential` for servers,
   `public` for CLI-only. Copy client ID + secret.
2. Register redirect URIs — `https://<host>/oauth/callback` (web, under `http.basePath`) and
   `http://127.0.0.1:0/callback` (CLI loopback, RFC 8252).
3. Create a resource server for `urn:yasaku:api`.
4. Set `oidc.issuer`, `oidc.clientID`, `oidc.clientSecret`, `oidc.resource: urn:yasaku:api`,
   `tokens.audience: urn:yasaku:api`, `tokens.issuer` and `tokens.jwksURL`.
5. Restart — log in at `/login`.
