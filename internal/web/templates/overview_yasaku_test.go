package templates

import (
	"strings"
	"testing"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/money"
)

// TestHomeDay_FollowsThePrototypeRows checks a day group: weekday heading, rows without the date but with a wallet badge, and the row-actions tip after the first row.
func TestHomeDay_FollowsThePrototypeRows(t *testing.T) {
	d := web.LayoutData{ActiveOrg: &web.ActiveOrg{Slug: "acme"}, Principal: &session.Principal{Email: "a@example.com"}}
	day := civil.Date{Year: 2026, Month: 11, Day: 15}
	rows := []TxRow{
		{ID: "a1", Kind: "expense", Note: "Bakso", CategoryName: "Food", WalletName: "GoPay", Day: day, Editable: true, Grouped: true, Amount: money.New(25000, money.IDR)},
		{ID: "b2", Kind: "expense", Note: "Kopi", CategoryName: "Coffee", WalletName: "Cash", Day: day, Editable: true, Grouped: true, Amount: money.New(28000, money.IDR)},
	}
	out := render(t, homeDay(d, YasakuOverviewView{ProjectSlug: "main"}, YasakuDay{Day: day, Today: true, Total: money.New(-53000, money.IDR), Rows: rows}, true))

	requireContains(t, out, "tx.today · ", `data-date-style="weekday"`, `badge badge-xs badge-ghost max-w-full"><span class="truncate">GoPay<`, ">Food<")
	if n := strings.Count(out, "data-ledger-date"); n != 1 {
		t.Errorf("want only the heading's date, got %d dates", n)
	}
	first, tip, second := strings.Index(out, ">Bakso<"), strings.Index(out, `data-tip="row-actions"`), strings.Index(out, ">Kopi<")
	if first < 0 || tip < first || second < tip {
		t.Errorf("the tip must sit between the first and second row: rows at %d and %d, tip at %d", first, second, tip)
	}
}

func TestHomePeriodNav_ADefaultNamedPeriodShowsTheLocalizedMonth(t *testing.T) {
	d := web.LayoutData{ActiveOrg: &web.ActiveOrg{Slug: "acme"}}
	start := civil.Date{Year: 2026, Month: 11, Day: 1}
	got := render(t, homePeriodNav(d, YasakuOverviewView{ProjectSlug: "p", Period: PeriodLabel{Name: "Nov 2026", Start: start, Monthly: true}}))
	requireContains(t, got, `datetime="2026-11-01"`, `data-date-style="month"`, `>November 2026</time>`)

	renamed := render(t, homePeriodNav(d, YasakuOverviewView{ProjectSlug: "p", Period: PeriodLabel{Name: "Payday cycle", Start: start}}))
	requireContains(t, renamed, `>Payday cycle</label>`)

	shared := render(t, homePeriodNav(d, YasakuOverviewView{ProjectSlug: "p", Period: PeriodLabel{Name: "Nov 2026", Start: civil.Date{Year: 2026, Month: 11, Day: 18}, Monthly: true, Shared: true}}))
	requireContains(t, shared, `period.label_from`)
}

func TestHomeQuickAdd_FourColumnsAtEveryWidth(t *testing.T) {
	got := render(t, homeQuickAdd(web.LayoutData{ActiveOrg: &web.ActiveOrg{Slug: "acme"}}, YasakuOverviewView{ProjectSlug: "p"}))
	requireContains(t, got, `class="grid grid-cols-4 gap-2"`)
}

func TestHomePeriodNav_SpansTheCardWithGhostChevrons(t *testing.T) {
	d := web.LayoutData{ActiveOrg: &web.ActiveOrg{Slug: "acme"}}
	got := render(t, homePeriodNav(d, YasakuOverviewView{ProjectSlug: "p", Period: PeriodLabel{Name: "Payday"}, PrevPeriodID: "a"}))
	requireContains(t, got, `class="join w-full"`, `class="btn btn-ghost btn-square join-item"`, `flex-1`)
	if strings.Contains(got, "self-start") || strings.Contains(got, `class="btn btn-square join-item"`) {
		t.Fatalf("period nav must span the card with ghost chevrons: %s", got)
	}
}
