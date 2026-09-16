# MCP

yasaku exposes its domain services over the Model Context Protocol as a fourth
presentation layer, alongside the SSR web handlers, the Connect-RPC API and the
scheduler. It holds no business logic: every tool calls the same `service.go`
method the web handler and the RPC service call.

## The endpoint

One endpoint per deployment:

```
<http.baseURL><http.basePath>/mcp
```

It is stateless streamable HTTP and accepts `POST` only. There is no
per-project URL — the org and project travel in each tool's `target` field, so
one registered resource server covers every project in the deployment.

Set `mcp.enabled=true` to mount it. See [CONFIGURATION.md](CONFIGURATION.md#mcp)
for `mcp.audience` and `mcp.audienceOverride`.

## Auth

MCP callers authenticate with a bearer JWT from
[authl](https://github.com/altalune-id/authl), never with a session cookie.

| Piece        | Value                                                                            |
| ------------ | -------------------------------------------------------------------------------- |
| Issuer       | `tokens.issuer`                                                                  |
| Audience     | `mcp.audience`, default `<baseURL><basePath>/mcp` (RFC 8707 resource)            |
| Signing keys | discovered from the issuer's JWKS at boot                                        |
| Scopes       | `yasaku:read` for read tools, `yasaku:write` for mutations                       |
| Metadata     | `/.well-known/oauth-protected-resource` and the RFC 9728 §3.1 path-suffixed form |

The MCP surface builds its **own** verifier from `tokens.*` but with
`mcp.audience` as the audience. A token minted for the Connect API is refused
here, and the reverse.

A request with no usable token gets `401` and a `WWW-Authenticate` header
naming the metadata URL:

```
WWW-Authenticate: Bearer resource_metadata="<baseURL>/.well-known/oauth-protected-resource<basePath>/mcp"
```

A token that authenticates but lacks the scope a tool needs is refused with the
tool name and the required scope, not with a 401.

### Operator setup in authl

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

Every mutating tool takes a `bool confirm`. The generator refuses at build time
to emit a tool annotated `mutation: true` whose request message has no `confirm`
field, so the convention cannot be forgotten.

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

## Errors

Tool errors carry the same envelope as the Connect API: the `apperror.v1.ErrorDetail`
shape with `code`, `meta`, `request_id` and `trace_id`. The `code` is the
append-only `<DOM><NNN>` identifier documented in
[ERROR_CODES.md](ERROR_CODES.md), so an agent can branch on it without parsing prose.

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

## Adding a tool

1. Annotate the RPC in `api/yasaku/v1/*.proto`:

   ```proto
   rpc RecordExpense(RecordExpenseRequest) returns (RecordExpenseResponse) {
     option (yasaku.mcp.v1.tool) = {
       name: "record_expense"
       description: "..."
       access: ACCESS_WRITE
       mutation: true
     };
   }
   ```

2. Run `make generate`. The local plugin `cmd/protoc-gen-yasaku-mcp` emits the
   tool registration into `gen/go/yasaku/v1/yasakuv1mcp`.
3. Add the domain to the `mcpDomains` manifest in `internal/boot/mcp.go`.
   `assertMCPWiring` fails the boot if an annotated service is never registered.

The plugin refuses to generate on five conditions, each a build failure rather
than a runtime surprise: a `mutation: true` request with no `bool confirm`
field, a tool name that is not lower snake case, a duplicate tool name,
`ACCESS_UNSPECIFIED`, and a streaming RPC.

## Testing the surface

Point the [MCP Inspector](https://github.com/modelcontextprotocol/inspector) at
the endpoint:

```bash
npx @modelcontextprotocol/inspector
```

Choose "Streamable HTTP", enter `<baseURL><basePath>/mcp`, and let it follow the
`WWW-Authenticate` challenge to the authorization server. With a token in hand
it lists the catalogue above and can call any tool.

For a token-free smoke test, request the metadata document, which is
unauthenticated:

```bash
curl -s "$BASE_URL/.well-known/oauth-protected-resource$BASE_PATH/mcp" | jq
```

## Template gaps fixed locally

Three changes live in this fork and belong upstream in the template:

- `user.User` gained `IDPIssuer` and `IDPSubject` so a bearer subject resolves
  to a local user without a session.
- `interceptor.Principal` assigned `IsAdmin` instead of OR-ing it, which
  escalated any caller whose token merely passed through an admin path.
- The MCP transport lives at `internal/mcp` and is exempted from the
  `domain-purity` depguard rules, since it is a transport surface and not a domain.
