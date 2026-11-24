package authz

import "altalune.id/yasaku/internal/platform/authn"

// The ledger permissions.
const (
	WalletsRead       Permission = "wallets:read"
	WalletsWrite      Permission = "wallets:write"
	CategoriesRead    Permission = "categories:read"
	CategoriesWrite   Permission = "categories:write"
	TransactionsRead  Permission = "transactions:read"
	TransactionsWrite Permission = "transactions:write"
	PeriodsClose      Permission = "periods:close"
	LedgerSettings    Permission = "ledger:settings"
)

func appDefs() []Def {
	return []Def{
		{Permission: WalletsRead, LabelKey: "authz.perm.wallets_read", Scope: authn.ScopeYasakuRead, Holder: HolderAny},
		{Permission: WalletsWrite, LabelKey: "authz.perm.wallets_write", Scope: authn.ScopeYasakuWrite, Holder: HolderAny},
		{Permission: CategoriesRead, LabelKey: "authz.perm.categories_read", Scope: authn.ScopeYasakuRead, Holder: HolderAny},
		{Permission: CategoriesWrite, LabelKey: "authz.perm.categories_write", Scope: authn.ScopeYasakuWrite, Holder: HolderAny},
		{Permission: TransactionsRead, LabelKey: "authz.perm.transactions_read", Scope: authn.ScopeYasakuRead, Holder: HolderAny},
		{Permission: TransactionsWrite, LabelKey: "authz.perm.transactions_write", Scope: authn.ScopeYasakuWrite, Holder: HolderAny},
		{Permission: PeriodsClose, LabelKey: "authz.perm.periods_close", Scope: authn.ScopeYasakuWrite, Holder: HolderAny},
		{Permission: LedgerSettings, LabelKey: "authz.perm.ledger_settings", Scope: authn.ScopeYasakuWrite, Holder: HolderAny},
		{Permission: ProjectsWrite, LabelKey: "authz.perm.projects_write", Scope: authn.ScopeYasakuWrite, Holder: HolderAny},
	}
}

func memberPermissions() []Permission {
	return []Permission{WalletsRead, WalletsWrite, CategoriesRead, CategoriesWrite, TransactionsRead, TransactionsWrite}
}
