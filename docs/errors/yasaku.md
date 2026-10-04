# yasaku error codes

Codes owned by the yasaku domain modules. The template codes live in [`README.md`](README.md); the
rules (append-only, `900`-`999` reserved) are the same, and `TestCodes_EveryRefIsDocumented` reads every
`docs/errors/*.md` file.

## LDG — Ledger settings

| Code     | Constant                             | Meaning                  |
| -------- | ------------------------------------ | ------------------------ |
| `LDG001` | `apperror.CodeLedgerInvalidTimezone` | Ledger Invalid Timezone  |
| `LDG002` | `apperror.CodeLedgerInvalidStartDay` | Ledger Invalid Start Day |
| `LDG003` | `apperror.CodeLedgerUnknownCurrency` | Ledger Unknown Currency  |

## WLT — Wallets

| Code     | Constant                           | Meaning               |
| -------- | ---------------------------------- | --------------------- |
| `WLT001` | `apperror.CodeWalletNotFound`      | Wallet Not Found      |
| `WLT002` | `apperror.CodeWalletInvalidName`   | Wallet Invalid Name   |
| `WLT003` | `apperror.CodeWalletAlreadyExists` | Wallet Already Exists |
| `WLT004` | `apperror.CodeWalletInvalidKind`   | Wallet Invalid Kind   |
| `WLT005` | `apperror.CodeWalletInUse`         | Wallet In Use         |
| `WLT006` | `apperror.CodeWalletAmbiguousName` | Wallet Ambiguous Name |
| `WLT007` | `apperror.CodeWalletArchived`      | Wallet Archived       |

## CTG — Transaction categories

| Code     | Constant                               | Meaning                    |
| -------- | -------------------------------------- | -------------------------- |
| `CTG001` | `apperror.CodeTxCategoryNotFound`      | Tx Category Not Found      |
| `CTG002` | `apperror.CodeTxCategoryInvalidName`   | Tx Category Invalid Name   |
| `CTG003` | `apperror.CodeTxCategoryAlreadyExists` | Tx Category Already Exists |
| `CTG004` | `apperror.CodeTxCategoryInvalidKind`   | Tx Category Invalid Kind   |
| `CTG005` | `apperror.CodeTxCategoryInUse`         | Tx Category In Use         |
| `CTG006` | `apperror.CodeTxCategoryAmbiguousName` | Tx Category Ambiguous Name |

## TXN — Transactions

| Code     | Constant                                       | Meaning                            |
| -------- | ---------------------------------------------- | ---------------------------------- |
| `TXN001` | `apperror.CodeTransactionNotFound`             | Transaction Not Found              |
| `TXN002` | `apperror.CodeTransactionInvalidAmount`        | Transaction Invalid Amount         |
| `TXN003` | `apperror.CodeTransactionCurrencyMismatch`     | Transaction Currency Mismatch      |
| `TXN004` | `apperror.CodeTransactionInvalidKind`          | Transaction Invalid Kind           |
| `TXN005` | `apperror.CodeTransactionSameWallet`           | Transaction Same Wallet            |
| `TXN006` | `apperror.CodeTransactionCategoryKindMismatch` | Transaction Category Kind Mismatch |
| `TXN007` | `apperror.CodeTransactionWalletArchived`       | Transaction Wallet Archived        |
| `TXN008` | `apperror.CodeTransactionPeriodLocked`         | Transaction Period Locked          |
| `TXN009` | `apperror.CodeTransactionPeriodNotAdjacent`    | Transaction Period Not Adjacent    |
| `TXN010` | `apperror.CodeTransactionInvalidNote`          | Transaction Invalid Note           |
| `TXN011` | `apperror.CodeTransactionSystemRecorded`       | Transaction System Recorded        |
| `TXN012` | `apperror.CodeTransactionAuthorMissing`        | Transaction Author Missing         |

## PRD — Periods

| Code     | Constant                             | Meaning                  |
| -------- | ------------------------------------ | ------------------------ |
| `PRD001` | `apperror.CodePeriodNotFound`        | Period Not Found         |
| `PRD002` | `apperror.CodePeriodInvalidName`     | Period Invalid Name      |
| `PRD003` | `apperror.CodePeriodInvalidRange`    | Period Invalid Range     |
| `PRD004` | `apperror.CodePeriodOverlap`         | Period Overlap           |
| `PRD005` | `apperror.CodePeriodAlreadyClosed`   | Period Already Closed    |
| `PRD006` | `apperror.CodePeriodNotClosed`       | Period Not Closed        |
| `PRD007` | `apperror.CodePeriodNotLatestClosed` | Period Not Latest Closed |
| `PRD008` | `apperror.CodePeriodAuthorMissing`   | Period Author Missing    |

## OSL — Opensheet mirror

| Code     | Constant                                    | Meaning                          |
| -------- | ------------------------------------------- | -------------------------------- |
| `OSL001` | `apperror.CodeOpensheetLinkNotFound`        | Opensheet Link Not Found         |
| `OSL002` | `apperror.CodeOpensheetInvalidSetting`      | Opensheet Invalid Setting        |
| `OSL003` | `apperror.CodeOpensheetAPIKeyRequired`      | Opensheet API Key Required       |
| `OSL004` | `apperror.CodeOpensheetSheetUnreachable`    | Opensheet Sheet Unreachable      |
| `OSL005` | `apperror.CodeOpensheetShapeMismatch`       | Opensheet Sheet Columns Missing  |
| `OSL006` | `apperror.CodeOpensheetNoIDColumn`          | Opensheet Sheet Has No id Column |
| `OSL007` | `apperror.CodeOpensheetSheetNotWritable`    | Opensheet Sheet Not Writable     |
| `OSL008` | `apperror.CodeOpensheetContractUnsatisfied` | Opensheet Table Contract Failed  |
| `OSL009` | `apperror.CodeOpensheetUnavailable`         | Opensheet Unavailable            |
| `OSL010` | `apperror.CodeOpensheetNotVerified`         | Opensheet Link Not Verified      |
| `OSL011` | `apperror.CodeOpensheetLinkDisabled`        | Opensheet Link Disabled          |
| `OSL012` | `apperror.CodeOpensheetSyncRefused`         | Opensheet Sync Refused           |
| `OSL013` | `apperror.CodeOpensheetScopeMismatch`       | Opensheet Job Scope Mismatch     |
| `OSL014` | `apperror.CodeOpensheetRowRefused`          | Opensheet Row Refused            |
| `OSL015` | `apperror.CodeOpensheetKeyUnreadable`       | Opensheet API Key Unreadable     |
| `OSL016` | `apperror.CodeOpensheetPrivateEndpoint`     | Opensheet Base URL Is Private    |

`OSL004` covers opensheet's 404 mask and a 401 or 403 during the Test: an unknown slug, a wrong org
or project, a revoked key, a key without the scope and a key not granted the sheet all look the same
from yasaku.

`OSL012` is a link-level sync refusal: it counts toward turning the link off. `OSL014` is a refusal of
one row: the row backs off and the link is never penalised. Which opensheet answer is which is in
[`opensheet/yasaku.md`](../opensheet/yasaku.md#failures).

`OSL015` means the deployment's `security.encryptionKey` no longer opens the saved API key (the key
was rotated). The person enters the opensheet key again and saves; the sync job treats it as a link-level
refusal.

`OSL016` means `opensheet.baseURL` resolves to a private address while `opensheet.allowPrivateHosts`
is off. Set `YASAKU_OPENSHEET_ALLOW_PRIVATE_HOSTS=true` when opensheet runs on a private network. The
Test answers it per tab; the sync job treats it as a link-level `config` refusal.

## MCP — Reserved

`MCP002`-`MCP004` were registered before the template sync and are retired. The registry is append-only,
so the numbers are never reused. They are not backticked here on purpose: the registry test treats a
backticked code in the first column as a live code.

| Code   | Former constant                  | Meaning                                |
| ------ | -------------------------------- | -------------------------------------- |
| MCP002 | `apperror.CodeMCPForbiddenScope` | Reserved, retired in the template sync |
| MCP003 | `apperror.CodeMCPUnknownUser`    | Reserved, retired in the template sync |
| MCP004 | `apperror.CodeMCPNotMember`      | Reserved, retired in the template sync |

A denied scope now carries `GEN003`; see the MCP section of [`README.md`](README.md).
