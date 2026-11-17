package templates

import (
	"testing"
	"time"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/web"
)

func TestTxRowItem_OpensItsDetailDrawerLazily(t *testing.T) {
	d := web.LayoutData{ActiveOrg: &web.ActiveOrg{Slug: "acme", Name: "Acme"}, CurrentPath: "/orgs/acme/projects/home/overview"}
	got := render(t, TxRowItem(d, "home", TxRow{ID: "abc", Kind: "expense", Note: "Kopi", Editable: true}))

	requireContains(t, got,
		`href="/orgs/acme/projects/home/transactions/abc" data-dialog-open="tx-detail-abc" data-dialog-src="/orgs/acme/projects/home/transactions/abc"`,
		`data-dialog-open="`+TxRowActionsID+`"`)
}

func TestLedgerDate_LongSpellsWeekdayMonthAndYear(t *testing.T) {
	day := civil.Date{Year: 2026, Month: time.November, Day: 15}
	got := render(t, LedgerDate(web.LayoutData{}, day, nil, Long()))

	requireContains(t, got, `data-date-style="long"`, `>Sunday, 15 November 2026</time>`)
}
