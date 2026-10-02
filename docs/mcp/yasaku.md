# yasaku MCP surface

What yasaku adds to the template MCP surface. Endpoint, auth, errors, adding a tool and testing are in
[`README.md`](README.md); this file holds only the yasaku tool set and its UI bundle.

## Operator setup in authl

1. Register yasaku as a resource server whose identifier is exactly
   `mcp.audience`, with the two scopes `yasaku:read` and `yasaku:write`.
2. Register a public PKCE client for each MCP host that will connect
   (Claude Desktop, an IDE, an agent runtime). Dynamic client registration is
   not yet available — see [authl#2](https://github.com/altalune-id/authl/issues/2).
3. Point `tokens.issuer` at the authl issuer URL and boot with `mcp.enabled=true`.

## Targeting a project

Every tool takes an optional `target` of `{org, project}` slugs. Resolution:

- Both given: they must name an org the caller belongs to and a project inside it.
- Omitted and the caller has exactly one org (or one project in the chosen org):
  it is selected automatically.
- Omitted and there is more than one: the tool answers with `needs`, listing the
  candidates, instead of guessing.

An org the caller does not belong to is reported exactly as an unknown slug is,
so the surface leaks no tenant names.

Call `list_projects` first when you are unsure what the caller can reach.

## Two-phase mutations

Every mutating tool takes a `bool confirm` in its request message. A mutating tool without one is a bug.

- `confirm` absent or false: the tool resolves names to ids, validates, and
  returns a `preview` of what it would do. Nothing is written.
- `confirm` true: the tool performs the write and returns a `result`.

A preview is the resolved intent, not the saved record. It carries no id and no
final timestamps, and a concurrent write landing between the two calls can
change what is actually saved. Tools say so in their own descriptions.

Two more response fields appear across the surface:

- `needs` — the call cannot proceed until the caller picks from `candidates`
  (an ambiguous wallet name, an unset target, an unaccepted icon).
- `warning` — the call succeeded but something is worth saying (an adjustment
  that wrote nothing because the balance already matched).

## Tool catalogue

27 tools across 7 services. `W` marks a mutation (takes `confirm`).

| Tool                      | Scope | W   | Purpose                                                               |
| ------------------------- | ----- | --- | --------------------------------------------------------------------- |
| `list_projects`           | read  |     | Ledgers the caller may reach; source of the `target` slugs.           |
| `now`                     | read  |     | Current date and time in the ledger's timezone, plus the open period. |
| `list_wallets`            | read  |     | Wallets with derived balances.                                        |
| `get_wallet`              | read  |     | One wallet plus its recent transactions.                              |
| `wallet_totals`           | read  |     | Spendable total, grand total, and the running period's flows.         |
| `create_wallet`           | write | ✓   | New wallet, optionally with an opening balance.                       |
| `update_wallet`           | write | ✓   | Rename or retype a wallet; currency is immutable.                     |
| `archive_wallet`          | write | ✓   | Retire a wallet, keeping its history.                                 |
| `adjust_balance`          | write | ✓   | Write one adjustment so the wallet matches a counted balance.         |
| `list_categories`         | read  |     | Expense and income categories.                                        |
| `create_category`         | write | ✓   | New category; kind is fixed at creation.                              |
| `seed_default_categories` | write | ✓   | Idempotently add the 13 expense and 7 income defaults.                |
| `list_recent_tx`          | read  |     | Recent transactions, filterable and cursor-paged.                     |
| `search_tx`               | read  |     | Note search with an optional inclusive date range.                    |
| `record_expense`          | write | ✓   | Money out of a wallet.                                                |
| `record_income`           | write | ✓   | Money into a wallet.                                                  |
| `record_transfer`         | write | ✓   | Money between own wallets; not an expense.                            |
| `record_batch`            | write | ✓   | Many transactions at once, each recorded independently.               |
| `revise_tx`               | write | ✓   | Change a recorded transaction.                                        |
| `delete_tx`               | write | ✓   | Remove a transaction permanently.                                     |
| `current_period`          | read  |     | The running period and its live totals.                               |
| `list_periods`            | read  |     | Periods newest first; the source of period ids.                       |
| `preview_close`           | read  |     | What closing on a given date would freeze.                            |
| `close_period`            | write | ✓   | Tutup buku: freeze totals and open the next period.                   |
| `reopen_period`           | write | ✓   | Reopen the latest closed period to fix it.                            |
| `period_report`           | read  |     | One period by category and by wallet.                                 |
| `cashflow_report`         | read  |     | Cashflow trend over recent periods, oldest first.                     |

Wherever a tool takes a wallet or a category it accepts a name or an id. A
**period is always an id** from `list_periods` or `current_period`, never a name.

## The resource `_meta`

The published resource carries its own `_meta.ui`, on both the `resources/list`
entry and the `resources/read` contents. `resources/read` is the normative
location — servers MAY omit UI resources from `resources/list` entirely.

```json
"_meta": { "ui": { "prefersBorder": false } }
```

`prefersBorder` is explicit because host defaults vary and the spec recommends
stating it: `app.css` paints a transparent body and `.ya-card` draws its own
border, so a host-drawn frame would double up on every card.

`mcp.UIResource` exposes this as a typed `PrefersBorder *bool`, not a raw map —
`$defs/McpUiResourceMeta` is `additionalProperties: false`, so the runtime owns
the wire shape and callers pass only values. `nil` leaves the host's default.

**All 27 tools carry the link.** The bundle routes on tool name: reads render a
card or chart, and the 14 mutations share one phase machine driven by the
`{needs, preview, result, warning}` envelope — `needs` renders an editable form
with the server's `candidates` as pickers, `preview` renders the resolved intent
plus a Confirm control, and `result` renders a receipt.

A commit is never rebuilt from the response. Several previews cannot round-trip
into their own request — `close_period`'s is a bare `Snapshot` with no period id,
and `adjust_balance`'s carries the computed delta rather than the target balance.
The bundle instead merges the original tool arguments (delivered on
`ui/notifications/tool-input`) with `confirm: true`, which is also how `target`
survives the round trip. The
bundle is one resource, `ui://yasaku/app`, assembled in `internal/mcp/ui` from
ordered source parts and published only when `mcp.appsUI=true`.

The vendored `ext-apps` bundle exports its names as aliases over minified
bindings, and an inlined module's exports are unreachable, so `ui.go` appends a
generated `globalThis.__extApps={...}` derived from the bundle's own `export`
list. `make mcp-ui-dev` writes the assembled document to `bin/bundle.html` for
local layout checks; it will not connect to a host.
