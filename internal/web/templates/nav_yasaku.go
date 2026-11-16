package templates

import "altalune.id/yasaku/internal/web"

// AppName is the product name the brand row and version line show.
const AppName = "Yasaku"

const brandIcon = "yasaku-mark-16.svg"

//i18n:use nav.home
//i18n:use nav.transactions
//i18n:use nav.reports
//i18n:use nav.categories
//i18n:use nav.periods
//i18n:use nav.opensheet
//i18n:use nav.ledger_settings
func projectNav() []navItem {
	return []navItem{
		{Key: "overview", Path: "/overview", Icon: "house", LabelKey: "nav.home", InDock: true},
		{Key: "transactions", Path: "/transactions", Icon: "receipt", LabelKey: "nav.transactions"},
		{Key: "reports", Path: "/reports", Icon: "chart-pie", LabelKey: "nav.reports", InDock: true},
		{Key: "wallets", Path: "/wallets", Icon: "wallet", LabelKey: "nav.wallets", InDock: true},
		{Key: "categories", Path: "/categories", Icon: "folder-tree", LabelKey: "nav.categories", More: true},
		{Key: "periods", Path: "/periods", Icon: "calendar-range", LabelKey: "nav.periods", More: true},
		{Key: "opensheet", Path: "/opensheet", Icon: "arrow-left-right", LabelKey: "nav.opensheet", More: true, Show: func(d web.LayoutData) bool { return d.Opensheet }},
		{Key: "settings", Path: "/settings", Icon: "settings", LabelKey: "nav.ledger_settings", More: true},
		{Key: "apikeys", Path: "/apikeys", Icon: "key", LabelKey: "nav.apikeys", More: true},
	}
}
