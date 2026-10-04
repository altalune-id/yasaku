# yasaku modules

The shape is in [`README.md`](README.md). These are the yasaku reference implementations to copy from:

- `internal/wallet/` — flat, with a multi-write workflow.
- `internal/transaction/` — relations and cross-module ports.

The other yasaku domain modules are `internal/period/`, `internal/report/`, `internal/category/` and
`internal/ledger/`. `internal/blog/` and `internal/todo/` are kept as code but mounted nowhere.

In a period's report, a wallet archived before the period ended (or before it was closed) is left out of the wallet balances and their totals, but its transactions still count in the period's income, expense and net.

## `internal/opensheetsync/` — optional

The Opensheet mirror ([`opensheet/yasaku.md`](../opensheet/yasaku.md)). It is mounted only when
`opensheet.baseURL` is set; otherwise boot builds none of its services, and no route, RPC, job or
scheduler job exists. It is the reference for an optional module with its own queue job and
scheduler job.

| File                                                                | Holds                                                                                                    |
| ------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- |
| `link.go`                                                           | the aggregate `Link` (one per project), `Settings`, `SheetSlugs`, `KeyHint`, `DisableAfter`              |
| `ref.go`                                                            | `Entity`, `Ref`, `LockOrder`, `State`, `MaxRowAttempts`, `RowBackoff`                                    |
| `contract.go`                                                       | the sheet contract `Contract()`, the one table behind the Test, the tutorial and the mapper              |
| `facts.go`, `mapper.go`                                             | the facts read from the domain services and their sheet rows                                             |
| `shape.go`                                                          | `CheckTab`, the Test's verdict on one tab's `/capabilities`                                              |
| `errors.go`, `classify.go`                                          | typed errors `OSL001`-`OSL016`; which opensheet answer is link-level, row-level or retried               |
| `store.go`, `postgres.go`, `pgwriter.go`, `sqlite.go`, `factory.go` | the `Store` port (links and `opensheet_sync_state`) and its adapters                                     |
| `mirror.go`                                                         | `Mirror`: `Mark` inside the write's unit of work, `Kick` after it, `KickDirty`, `Reconcile`              |
| `job.go`, `sync.go`, `source.go`                                    | the `opensheet.sync` v1 payload; `Syncer`, which pushes rows; the `Source` port over the domain services |
| `service.go`                                                        | `Service`: `Status`, `Test`, `Save`, `SetEnabled`, `SyncNow`, `Delete`                                   |
| `secret.go`, `endpoint.go`                                          | the sealed API key; the opensheet client built from server config                                        |
| `consumer.go`, `scheduler.go`                                       | the `queue.Provider` for `opensheet.sync`; the `scheduler.Provider` for `opensheet-reconcile`            |

The transaction, wallet and category modules each declare their own `Mirror` port (`mirror.go`) and
never import `opensheetsync`; boot adapts them in `internal/boot/opensheet_yasaku.go`. Their writes
always run in the tenant unit of work, mounted or not. The console page is
`internal/web/handlers/opensheet_yasaku.go`; the RPC is `yasaku.v1.OpensheetService`, whose six
procedures exist only when `opensheet.baseURL` is set (`/api/openapi.yaml` still lists them, template
gap T70).
