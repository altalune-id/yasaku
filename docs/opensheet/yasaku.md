# Opensheet mirror

A one-way mirror of a project's wallets, categories and transactions into three tabs of a Google
Sheet, written through [opensheet](https://github.com/altalune-id/opensheet). yasaku's database is
the source of truth; the sheet is a copy. Module: `internal/opensheetsync`. Client: `opensheet/`.

## Turning it on

| Level   | Switch                                                                       | Effect when off                                                                                     |
| ------- | ---------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------- |
| Server  | `opensheet.baseURL` (`YASAKU_OPENSHEET_BASE_URL`)                            | module unmounted: no nav entry, console routes, RPCs, `opensheet.sync` job or `opensheet-reconcile` |
| Server  | `opensheet.allowPrivateHosts` (`YASAKU_OPENSHEET_ALLOW_PRIVATE_HOSTS`)       | the base URL must be `https` and must not resolve to a private or loopback address                  |
| Server  | `security.encryptionKey` (`YASAKU_SECURITY_ENCRYPTION_KEY`)                  | required when `baseURL` is set; it seals each project's opensheet API key                           |
| Project | the Enabled switch on the Opensheet page (`SetOpensheetLinkEnabled` over S2) | nothing is marked, kicked or reconciled for the project                                             |

SECURITY: never rotate `security.encryptionKey` once links exist. Every saved opensheet key stops
opening (`OSL015`) and each link turns itself off.

Both `opensheet.*` keys are `bootstrap` config: one opensheet server per deployment, never a tenant
field. Config validation runs first: it refuses a malformed `baseURL` (`http_url`) and, on Postgres,
an empty `security.encryptionKey` (sessions need it too). Then boot refuses to start when:

| Condition (with `baseURL` set)                                              | Refused with                           |
| --------------------------------------------------------------------------- | -------------------------------------- |
| `security.encryptionKey` is empty                                           | `cannot be sealed`                     |
| the URL does not parse or has no host                                       | `opensheet.baseURL is not a valid URL` |
| the scheme is not `https` and `allowPrivateHosts` is off                    | `must be https`                        |
| the host is a literal private or loopback IP and `allowPrivateHosts` is off | `is refused`                           |

NOTE: a host name that only resolves to a private address (`*.railway.internal`) still passes boot.
The client's dialer refuses it at runtime: the Test shows `OSL016`, and each enabled link turns itself
off after 3 syncs. Set `allowPrivateHosts=true` for it.
A host that resolves to both public and private addresses, with a private one first, is also refused as `OSL016`: set the flag, or use a host that resolves only to public addresses.

Who: an org owner or admin changes the link (Test, Save, the switch, Sync everything now, Remove).
Any project member reads it. An API key principal is refused every change, even with `yasaku:write`.

## The sheet contract

One Go table, `opensheetsync.Contract()`, drives the Test, the tutorial and the row mapper.
`TestContract_IsPinned` guards it: changing a column breaks every linked sheet.

| Tab (default slug)    | Columns, in order                                                                                                                                                                                      |
| --------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `yasaku-transactions` | `id`, `date`, `occurred_at`, `kind`, `amount`, `currency`, `wallet`, `wallet_id`, `to_wallet`, `to_wallet_id`, `category`, `category_id`, `note`, `period`, `recurring_id`, `updated_at`, `deleted_at` |
| `yasaku-wallets`      | `id`, `name`, `kind`, `provider`, `currency`, `balance`, `exclude_from_total`, `archived`, `updated_at`, `deleted_at`                                                                                  |
| `yasaku-categories`   | `id`, `name`, `kind`, `icon`, `color`, `archived`, `updated_at`, `deleted_at`                                                                                                                          |

- Every tab needs `id` as its id column, must be writable, and needs a `deleted_at` column (U4).
  opensheet writes `deleted_at` when yasaku deletes a row; yasaku never writes it. opensheet refuses a
  delete on a tab without it (`SHT029`).
- Each tab has its own sheet slug; two tabs on one slug is `OSL002`.
- `date` is the ledger day in the project timezone, `occurred_at` carries that zone's offset,
  `updated_at` is UTC, money is in major units.

## Setting up a project

The Opensheet page shows three steps, each header row with a copy button (tab-separated, so one paste
fills row 1):

1. Create a Google Sheet with three tabs and paste each header row into row 1.
2. In opensheet, publish each tab with its slug, set `id` as the id column, mark it writable.
3. Mint an opensheet API key with `sheets:read` and `sheets:write`, granted to those three sheets.

Then enter the opensheet org and project slugs, the key and the three sheet slugs.

**Test** calls opensheet's `/capabilities` for the three tabs at once (10s in total) and checks each:

| Check          | Fails with | When                                                                                             |
| -------------- | ---------- | ------------------------------------------------------------------------------------------------ |
| Reachable      | `OSL004`   | a 404 (opensheet's mask: wrong slug, org or project, revoked key, sheet not granted), 401 or 403 |
| Reachable      | `OSL009`   | opensheet did not answer: 5xx, rate limit, network                                               |
| Reachable      | `OSL016`   | the base URL resolves to a private address and `allowPrivateHosts` is off                        |
| Columns        | `OSL005`   | a contract column is missing from the reported columns (`deleted_at` included)                   |
| id column      | `OSL006`   | `id` is not the id column                                                                        |
| Writable       | `OSL007`   | the sheet is read only                                                                           |
| Table contract | `OSL008`   | opensheet's own table contract fails, or it has not validated an empty tab yet                   |

**Save** runs the same Test again on the server and stores the settings only when every tab passes.
The console answers 422 with the checklist; over the API the error is the first failing tab's code.
The typed key is never echoed back. The first Save needs a key (`OSL003`); an empty key field
later keeps the saved key. The key is sealed with
`security.encryptionKey`, bound to the org and project, and only its last four characters are shown
again. A passing Save sets `verified_at` and resets the failure streak to 0; it keeps `last_error`,
which the page shows next to the Test.

**Enabled** needs a verified link (`OSL010`) and a key that still opens (`OSL015`). **Sync everything
now** needs the link on (`OSL011`). **Remove** deletes the link and its sync state; the sheet's rows
stay.

### Empty tabs: columns are deferred

opensheet reports `columns` from the tab's saved header row. A sheet published before that opensheet
change and not refreshed since, or never read, falls back to the keys of its first live row, so an
empty tab reports `[]` or `[deleted_at]`. Then the Test cannot see the columns:

- It marks the tab "columns deferred" (`columns_deferred` over S2) and skips the Columns check.
- It still requires soft delete (`deleted_at`), writable, and opensheet's contract verdict with
  `validatedAt` set. Without `validatedAt` the tab fails with `OSL008` ("open its rows in opensheet
  once, then run Test again").
- The first sync checks the columns. A missing column comes back as `400 SHT014`, a link-level
  refusal, and the page shows it next to the Test. Until a sync lands after the Save, the status says
  "No sync has completed since the last save" (`awaiting_first_sync`).

Rollout: until the opensheet server runs the header-row change and has refreshed a sheet, its empty
tabs are deferred. After that, an empty tab is checked strictly, like a populated one.

### Stale header

`capabilities` never contacts Google. A header fixed in Google after opensheet's last refresh still
fails the Columns check. The `OSL005` message says so: open the tab's rows in opensheet (or wait a
few minutes), then run Test again. yasaku does not purge opensheet's cache: that needs
`cache:purge`, which the tutorial key does not have.

## How rows reach the sheet

1. **Mark**, inside the write's unit of work, only when the project's link is enabled (the link row is
   read `FOR SHARE` on Postgres, so Enable and Remove wait for the write). It bumps the row's
   `version` in `opensheet_sync_state` and resets its `attempts` and `retry_after`. Renaming a wallet
   or category also marks every transaction that shows its name. Rows are locked transactions, then
   categories, then wallets, each by id.
2. **Kick**, after the commit, detached from the request. It submits one `opensheet.sync` job through
   `svcs.jobs` with at most the first 50 refs; the rest stay dirty.
3. **The job** leases each row for 60s, reads its current state through the domain services, and
   PATCHes it (no `id`, no `deleted_at` in the body). A missing row (`SHT013`) is created with
   `Idempotency-Key: <id>:<version>`; `SHT012` or `SHT018` means an earlier create landed, so it
   PATCHes. A deleted row is DELETEd (tombstoned). It settles `synced_version` by compare-and-set;
   a mark during the push makes it push again, at most 3 times. It stops starting rows 10s before
   its deadline.
4. **Fast drain (U2)**, queue on only: a job that got a full batch of 50, processed all of them and
   pushed at least one, submits one follow-up job with the next 50 claimable dirty rows. Inline jobs
   never chain.
5. **The reconciler** `opensheet-reconcile` runs every 5 minutes (30s jitter) per org, singleton,
   60s per org. It re-submits rows still dirty a minute after their last mark or failed push that
   are not leased, not waiting on `retry_after` and not given up: one page of 50 per project when queued, up to two
   pages (100) per project inline. Each tick starts one project later, so a long backlog cannot keep
   the others waiting.
6. **The backfill** marks every wallet, category and transaction, then kicks one job of the oldest
   50 dirty rows. It runs on every off→on, on a Save that points an enabled link at other sheets, and
   on "Sync everything now".

| `queue.enabled` | A write waits for                         | A backfill drains                                     |
| --------------- | ----------------------------------------- | ----------------------------------------------------- |
| `true`          | the publish only                          | 50 rows per job, job after job (fast drain)           |
| `false`         | one inline job of up to 50 rows (20s cap) | 50 rows at once, then 100 per project every 5 minutes |

## Failures

| Kind       | Examples                                                                                                                                                                                                                                                                                  | What happens                                                                                                                                      |
| ---------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| Link-level | any 404 except a missing row (the `SHT001` mask, `SHT008`); 401, 403 (`SHT009`); 424 (`CRD006`, expired Google credential); `SHT010`, `SHT011`, `SHT014`, `SHT021`, `SHT023`, `SHT029`; an unreadable key (`OSL015`); a private base URL without `allowPrivateHosts` (`config`, `OSL016`) | `OSL012`. The job stops and dead-letters. The link records `last_error` and `failure_streak + 1`. The row is not charged                          |
| Row-level  | 400 or 422 validation (`SHT016`), 412, 413, any other 4xx                                                                                                                                                                                                                                 | `OSL014`. The row records `attempts`, `last_error` and `retry_after` (1, 2, 4, then 8 minutes); the job goes on. Never counts toward auto-disable |
| Transient  | 429, 5xx, 409 `SHT017`, network, timeout                                                                                                                                                                                                                                                  | The row records `last_error` only. The job returns an error: the queue retries it, or inline, the reconciler picks the row up                     |

- **Auto-disable:** 3 link-level refusals in a row (`DisableAfter`) turn the link off, clear
  `verified_at` and set `auto_disabled_at`. The page asks for a new Test. Recover: fix the sheet,
  Test, Save, turn it on.
- **Streak:** a job that pushed at least one row clears the streak and `last_error` and sets
  `last_synced_at`. A passing Save resets the streak. Turning the link on clears the streak,
  `last_error` and `auto_disabled_at`. A job that loaded the link before it was saved again is
  ignored. A link refusal after some rows were pushed does not move `last_synced_at`.
- **Given up:** after 5 refusals (`MaxRowAttempts`) a row leaves the pending count and shows as
  "N rows given up". Its next edit or "Sync everything now" resets it.
- **429:** `RateLimitedError.RetryAfter` is ignored. The job is retried and the queue's backoff (or,
  inline, the reconciler) decides when.
- **Encryption key change:** a rotated `security.encryptionKey` no longer opens the sealed keys. Each
  sync is then a link-level refusal (`key`, `OSL015`) and the link turns itself off; Test, Enable and
  a Save with an empty key answer `OSL015`. Recover: enter the opensheet key again and Save.
- **Safe text only:** `last_error` on the link and the row holds the typed message, the opensheet
  code and opensheet's own message, never a cause's text (hosts, URLs, sealer detail). The full
  error is logged: Warn for a link refusal and a refused row, Debug for the rest.
- The handler sets `SuppressReport` and has no `OnDeadLetter`: a failure is a user problem shown on
  the page, not an incident.
- **Streak resets:** any job that pushes at least one row and ends without an error records a
  success, which clears `last_error` and resets the link's `failure_streak`. A link-level refusal
  that hits only one tab can therefore come and go from the page while the other tabs keep syncing,
  and it may never reach auto-disable. Saving unchanged settings again also hides `last_error` (the
  page shows it only while `failure_streak > 0`) until the next sync is refused.

## API

`yasaku.v1.OpensheetService` on the control plane (S2), six procedures: `GetOpensheetLink`
(`yasaku:read`), `TestOpensheetLink`, `SaveOpensheetLink`, `SetOpensheetLinkEnabled`,
`SyncOpensheetNow` and `DeleteOpensheetLink` (`yasaku:write` and the owner or admin role). They
exist only when `opensheet.baseURL` is set. NOTE (template gap T70): `/api/openapi.yaml` still lists
them when it is empty, because `openAPI()` does not prune unmounted paths. The console routes are
under `/orgs/{org}/projects/{project}/opensheet`.

## Limits

- One-way only; edits made in the sheet are overwritten by the next push of that row.
- A period rename does not cascade; the `period` cell refreshes on the next edit or "Sync everything now".
- A ledger timezone change does not cascade either; the `date` and `occurred_at` cells refresh on
  the next edit or "Sync everything now".
- With the queue off, a write waits for its sync inline (one job per write, under the 20s budget);
  production runs the queue.
- No MCP tools (an LLM host should not handle an opensheet key).
- Scopes: `yasaku:read` / `yasaku:write` enforced; `opensheet:read` / `opensheet:write` declared, TODO.
