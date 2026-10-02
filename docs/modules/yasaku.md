# yasaku modules

The shape is in [`README.md`](README.md). These are the yasaku reference implementations to copy from:

- `internal/wallet/` — flat, with a multi-write workflow.
- `internal/transaction/` — relations and cross-module ports.

The other yasaku domain modules are `internal/period/`, `internal/report/`, `internal/category/` and
`internal/ledger/`. `internal/blog/` and `internal/todo/` are kept as code but mounted nowhere.

In a period's report, a wallet archived before the period ended (or before it was closed) is left out of the wallet balances and their totals, but its transactions still count in the period's income, expense and net.
