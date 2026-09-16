# Error codes

Every user-visible failure carries a code. It is shown on the error page and on inline form errors,
next to the request id, so a report can be matched to a log line.

Codes are `<DOM><NNN>` — a three-letter domain mnemonic plus a per-domain sequence. They are
**append-only**: a code is never renumbered and a retired code is never reused, because users quote
them from screenshots. `NNN` in the range `900`-`999` is reserved for unexpected or internal failures.

`docs/ERROR_CODES.md` is verified against `internal/apperror/codes.go` by `TestCodes_EveryRefIsDocumented`.

## GEN — General / cross-cutting

| Code     | Constant                       | Meaning          |
| -------- | ------------------------------ | ---------------- |
| `GEN001` | `apperror.CodeTenantMissing`   | Tenant Missing   |
| `GEN002` | `apperror.CodeUnauthenticated` | Unauthenticated  |
| `GEN003` | `apperror.CodeForbidden`       | Forbidden        |
| `GEN004` | `apperror.CodeValidation`      | Validation       |
| `GEN005` | `apperror.CodeNotFound`        | Not Found        |
| `GEN006` | `apperror.CodeAlreadyExists`   | Already Exists   |
| `GEN900` | `apperror.CodeUnexpectedError` | Unexpected Error |

## USR — Users

| Code     | Constant                         | Meaning             |
| -------- | -------------------------------- | ------------------- |
| `USR001` | `apperror.CodeUserNotFound`      | User Not Found      |
| `USR002` | `apperror.CodeUserAlreadyExists` | User Already Exists |
| `USR003` | `apperror.CodeUserNotInvited`    | User Not Invited    |
| `USR004` | `apperror.CodeUserInvalidEmail`  | User Invalid Email  |
| `USR005` | `apperror.CodeUserInvalidName`   | User Invalid Name   |

## ORG — Organizations

| Code     | Constant                            | Meaning                |
| -------- | ----------------------------------- | ---------------------- |
| `ORG001` | `apperror.CodeOrgNotFound`          | Org Not Found          |
| `ORG002` | `apperror.CodeOrgAlreadyExists`     | Org Already Exists     |
| `ORG003` | `apperror.CodeOrgInvalidSlug`       | Org Invalid Slug       |
| `ORG004` | `apperror.CodeOrgInvalidName`       | Org Invalid Name       |
| `ORG005` | `apperror.CodeOrgMembershipExists`  | Org Membership Exists  |
| `ORG006` | `apperror.CodeOrgMembershipMissing` | Org Membership Missing |
| `ORG007` | `apperror.CodeOrgCreationDisabled`  | Org Creation Disabled  |
| `ORG008` | `apperror.CodeOrgSystemProtected`   | Org System Protected   |
| `ORG009` | `apperror.CodeOrgSelfRemoval`       | Org Self Removal       |
| `ORG010` | `apperror.CodeOrgOwnerRemoval`      | Org Owner Removal      |

## PRJ — Projects

| Code     | Constant                              | Meaning                  |
| -------- | ------------------------------------- | ------------------------ |
| `PRJ001` | `apperror.CodeProjectNotFound`        | Project Not Found        |
| `PRJ002` | `apperror.CodeProjectAlreadyExists`   | Project Already Exists   |
| `PRJ003` | `apperror.CodeProjectInvalidSlug`     | Project Invalid Slug     |
| `PRJ004` | `apperror.CodeProjectSystemProtected` | Project System Protected |

## INV — Invites

| Code     | Constant                         | Meaning             |
| -------- | -------------------------------- | ------------------- |
| `INV001` | `apperror.CodeInviteNotFound`    | Invite Not Found    |
| `INV002` | `apperror.CodeInviteExpired`     | Invite Expired      |
| `INV003` | `apperror.CodeInviteAlreadyUsed` | Invite Already Used |
| `INV004` | `apperror.CodeInviteInvalidRole` | Invite Invalid Role |
| `INV005` | `apperror.CodeInviteDisabled`    | Invite Disabled     |

## TDO — Todos

| Code     | Constant                          | Meaning              |
| -------- | --------------------------------- | -------------------- |
| `TDO001` | `apperror.CodeTodoNotFound`       | Todo Not Found       |
| `TDO002` | `apperror.CodeTodoInvalidTitle`   | Todo Invalid Title   |
| `TDO003` | `apperror.CodeTodoAlreadyDeleted` | Todo Already Deleted |

## SGN — Signup

| Code     | Constant                      | Meaning         |
| -------- | ----------------------------- | --------------- |
| `SGN001` | `apperror.CodeSignupRequired` | Signup Required |

## AUT — Authentication

| Code     | Constant                              | Meaning                  |
| -------- | ------------------------------------- | ------------------------ |
| `AUT001` | `apperror.CodeAuthInvalidCredentials` | Auth Invalid Credentials |
| `AUT002` | `apperror.CodeAuthOIDCUnavailable`    | Auth OIDC Unavailable    |
| `AUT003` | `apperror.CodeAuthOIDCClaimMissing`   | Auth OIDC Claim Missing  |

## TKN — Tokens

| Code     | Constant                    | Meaning       |
| -------- | --------------------------- | ------------- |
| `TKN001` | `apperror.CodeTokenExpired` | Token Expired |

## ONB — Onboarding

| Code     | Constant                             | Meaning                 |
| -------- | ------------------------------------ | ----------------------- |
| `ONB001` | `apperror.CodeOnboardingRequired`    | Onboarding Required     |
| `ONB002` | `apperror.CodeOnboardingAlreadyDone` | Onboarding Already Done |

## ENC — Encryption at rest

| Code     | Constant                             | Meaning                |
| -------- | ------------------------------------ | ---------------------- |
| `ENC001` | `apperror.CodeEncryptionUnavailable` | Encryption Unavailable |
| `ENC002` | `apperror.CodeEncryptionOpenFailed`  | Encryption Open Failed |

## BLG — Blog posts

| Code     | Constant                            | Meaning                |
| -------- | ----------------------------------- | ---------------------- |
| `BLG001` | `apperror.CodePostNotFound`         | Post Not Found         |
| `BLG002` | `apperror.CodePostAlreadyExists`    | Post Already Exists    |
| `BLG003` | `apperror.CodePostInvalidTitle`     | Post Invalid Title     |
| `BLG004` | `apperror.CodePostInvalidSlug`      | Post Invalid Slug      |
| `BLG005` | `apperror.CodePostInvalidBody`      | Post Invalid Body      |
| `BLG006` | `apperror.CodePostCategoryRequired` | Post Category Required |

## CAT — Blog categories

| Code     | Constant                             | Meaning                 |
| -------- | ------------------------------------ | ----------------------- |
| `CAT001` | `apperror.CodeCategoryNotFound`      | Category Not Found      |
| `CAT002` | `apperror.CodeCategoryAlreadyExists` | Category Already Exists |
| `CAT003` | `apperror.CodeCategoryInvalidName`   | Category Invalid Name   |
| `CAT004` | `apperror.CodeCategoryInUse`         | Category In Use         |

## TAG — Blog tags

| Code     | Constant                        | Meaning            |
| -------- | ------------------------------- | ------------------ |
| `TAG001` | `apperror.CodeTagNotFound`      | Tag Not Found      |
| `TAG002` | `apperror.CodeTagAlreadyExists` | Tag Already Exists |
| `TAG003` | `apperror.CodeTagInvalidName`   | Tag Invalid Name   |
| `TAG004` | `apperror.CodeTagInUse`         | Tag In Use         |

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

## MCP — MCP surface

| Code     | Constant                          | Meaning             |
| -------- | --------------------------------- | ------------------- |
| `MCP001` | `apperror.CodeMCPUnauthenticated` | MCP Unauthenticated |
| `MCP002` | `apperror.CodeMCPForbiddenScope`  | MCP Forbidden Scope |
| `MCP003` | `apperror.CodeMCPUnknownUser`     | MCP Unknown User    |
| `MCP004` | `apperror.CodeMCPNotMember`       | MCP Not Member      |
