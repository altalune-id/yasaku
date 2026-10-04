# yasaku deployment

The template page is [`deployment`](README.md); workers and the `serve` flag matrix are in
[`workers`](workers.md). This page is how yasaku runs in production. Where it disagrees with the
template's "Railway's NATS template" note, this page wins.

## NATS on Railway

Production enables the queue. NATS is a separate Railway service built from
[`altalune-id/nats`](https://github.com/altalune-id/nats): the official `nats` image pinned by
digest, JetStream on `/data`, one server shared by every app with one NATS account per app. yasaku
is user `yasaku` in account `YASAKU`, capped at 1G of JetStream file storage.

1. Check that the pinned tag exists in GHCR before deploying. It must include altalune-id/nats#6
   (per-app accounts); `cdc9440` predates it and takes only a token:

   ```bash
   curl -s -H "Authorization: Bearer $(curl -s 'https://ghcr.io/token?scope=repository:altalune-id/nats:pull' | jq -r .token)" \
     https://ghcr.io/v2/altalune-id/nats/tags/list
   ```

2. New service from image `ghcr.io/altalune-id/nats:<short-sha>`. Never `edge`.

   NOTE: switch to the `X.Y.Z` image tag (git tag `vX.Y.Z`) once altalune-id/nats cuts a release.

3. Attach a volume at `/data`, at least 5 GB. Without it every redeploy drops `WORK` and `DLQ`. The
   streams reserve 576 MiB of the account's quota at creation (`WORK` 256 + `DLQ` 256 + `BROADCAST`
   64 MiB). Past the quota, yasaku's boot fails with `insufficient storage resources available (10047)`.
4. Set every `<APP>_NATS_PASSWORD` the image's config names, `YASAKU_NATS_PASSWORD` among them, to a
   long random string. The server refuses to start while any is unset, empty or under 16 characters.
5. Private networking only: no public domain, no TCP proxy.
6. NATS service stop timeout about 20s.

On every yasaku service (`<svc>` is the NATS service's Railway name):

```bash
YASAKU_QUEUE_ENABLED=true
YASAKU_QUEUE_URL=nats://<svc>.railway.internal:4222
YASAKU_QUEUE_USER=yasaku
YASAKU_QUEUE_PASSWORD=${{<svc>.YASAKU_NATS_PASSWORD}}
```

Also set the yasaku service's stop timeout to about 20s (consumer drain 8s, then connection drain).

SECURITY: the consumer trusts the tenant headers on a message, so anyone who can publish in the
`YASAKU` account can act as any tenant. The password is as sensitive as `YASAKU_DB_DSN`. Never put
another app in the `YASAKU` account: they share stream names and would consume each other's jobs.

## Boot and outages

| Situation                                | What happens                                                                                                                     | Do                                                                                  |
| ---------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------- |
| NATS unreachable at boot                 | boot retries for `queue.connectTimeout` (10s), then exits `1`: `boot: queue: queue: connect within 10s: …`. Railway restarts it. | check the NATS service is up, the URL host and the user and password                |
| NATS lost after boot                     | the client reconnects forever; HTTP keeps serving                                                                                | nothing; watch logs for `queue: disconnected` / `queue: reconnected`                |
| `Submit` during an outage longer than 5s | `*queue.PublishError`, reported as `<module>.<Method>: submit`; the user's change is saved, the job is lost                      | the module's reconciler (opensheet) re-submits; otherwise re-run the action         |
| a consume loop closes                    | `*queue.ConsumerClosedError` stops the process; the restart recreates streams and consumers                                      | nothing                                                                             |
| a job fails 5 times or permanently       | it moves to `DLQ` (30 days) and is reported as `queue: dead-lettered`                                                            | inspect with the `nats` CLI from a shell on the private network; no replay tool yet |

NOTE: `opensheet.sync` dead letters from link-level refusals are expected, not incidents. The job sets
`SuppressReport`, so each one logs a Warn only; the cause is shown on the project's Opensheet page.

`/readyz` reports the database only; the template has no queue readiness check. To probe NATS, call
its monitoring endpoint from inside the private network:
`curl http://<svc>.railway.internal:8222/healthz?js-enabled-only=true`.

## Turning the queue off

Set `YASAKU_QUEUE_ENABLED=false` and redeploy. Yasaku jobs then run inline in the request
([`queue/yasaku.md`](../queue/yasaku.md)); nothing is dropped. Jobs already in `WORK` stay there
until the queue is turned back on (7-day `MaxAge`). Boot logs the template's
`queue: disabled — Submit is a no-op` and then yasaku's inline notice; both are expected. With more than one replica, the onboarding gate
needs the queue ([`workers`](workers.md#queue)).

## Opensheet

The Opensheet mirror ([`opensheet/yasaku.md`](../opensheet/yasaku.md)) is off unless the base URL is
set. In production, on every yasaku service:

```bash
YASAKU_OPENSHEET_BASE_URL=https://<opensheet host>   # or http://<svc>.railway.internal:<port>
YASAKU_OPENSHEET_ALLOW_PRIVATE_HOSTS=true            # only when opensheet is on the private network
YASAKU_SECURITY_ENCRYPTION_KEY=<32 bytes, hex or base64>
```

- When opensheet runs in the same Railway project, use its private host. `*.railway.internal` resolves
  to a private address, which the client refuses unless `YASAKU_OPENSHEET_ALLOW_PRIVATE_HOSTS=true`.
  That flag is also what allows plain `http://`; without it the base URL must be `https://`.
- Leave `YASAKU_OPENSHEET_ALLOW_PRIVATE_HOSTS` unset for a public opensheet host.
- Config validation refuses a malformed base URL and, on Postgres, an empty encryption key. Then boot
  exits when the base URL is set and the encryption key is empty, when the URL is not `https` without
  the flag, and when it is a literal private IP without the flag.
- A private host name without the flag still passes boot. The Test then shows `OSL016`, and each sync
  is a link-level `config` refusal, so each enabled link turns itself off after 3 syncs.
- Run the queue in production: with it off, a write in a project with the mirror on waits for its
  sync inline.
- The sheets' header rows are checked by opensheet's `/capabilities`. Until the opensheet server runs
  its header-row change and has refreshed a sheet, Test cannot see the columns of an empty tab and
  leaves them to the first sync.
- After the deploy, open a project: its nav shows "Opensheet" when the module is mounted.

SECURITY: `YASAKU_SECURITY_ENCRYPTION_KEY` is already required on Postgres, because it seals sessions.
Set it once and never change it: a new key logs everyone out and makes every saved opensheet key
unreadable (`OSL015`), so each link turns itself off after 3 syncs until its project's key is entered
again and saved. Generate one with `openssl rand -hex 32`.

## Local dev

The queue is off by default, so jobs run inline. To run against NATS:

```bash
podman compose up -d nats   # or: docker compose up -d nats
YASAKU_QUEUE_ENABLED=true YASAKU_QUEUE_URL=nats://127.0.0.1:4222 YASAKU_QUEUE_TOKEN=yasaku-dev make dev
podman compose rm -sf nats && rm -rf docker/data/nats
```
