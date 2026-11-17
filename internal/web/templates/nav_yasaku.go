package templates

import "altalune.id/yasaku/internal/web"

// AppName is the product name the brand row and version line show.
const AppName = "Yasaku"

const brandIcon = "yasaku-mark-16.svg"

const addTipID = "add-first"

const moreKey = "more"

//i18n:use nav.home
//i18n:use nav.wallets
//i18n:use nav.reports
//i18n:use nav.categories
//i18n:use nav.periods
//i18n:use nav.opensheet
//i18n:use nav.ledger_settings
//i18n:use more.desc_categories
//i18n:use more.desc_periods
//i18n:use more.desc_opensheet
//i18n:use more.desc_settings
//i18n:use more.desc_apikeys
func projectNav() []navItem {
	return []navItem{
		{Key: "overview", Path: "/overview", Icon: "house", LabelKey: "nav.home", InDock: true},
		{Key: "reports", Path: "/reports", Icon: "chart-pie", LabelKey: "nav.reports", InDock: true},
		{Key: "wallets", Path: "/wallets", Icon: "wallet", LabelKey: "nav.wallets", InDock: true},
		{Key: "opensheet", Path: "/opensheet", Icon: "sheet", LabelKey: "nav.opensheet", DescKey: "more.desc_opensheet", More: true, Show: func(d web.LayoutData) bool { return d.Opensheet }},
		{Key: "categories", Path: "/categories", Icon: "tags", LabelKey: "nav.categories", DescKey: "more.desc_categories", More: true},
		{Key: "periods", Path: "/periods", Icon: "calendar-check", LabelKey: "nav.periods", DescKey: "more.desc_periods", More: true},
		{Key: "settings", Path: "/settings", Icon: "settings", LabelKey: "nav.ledger_settings", DescKey: "more.desc_settings", More: true},
		{Key: "apikeys", Path: "/apikeys", Icon: "key", LabelKey: "nav.apikeys", DescKey: "more.desc_apikeys", More: true, MoreOnly: true},
	}
}
