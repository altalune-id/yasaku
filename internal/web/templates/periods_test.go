package templates

import (
	"regexp"
	"strings"
	"testing"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/web"
)

func periodsViewFixture() PeriodsView {
	end := civil.Date{Year: 2026, Month: 10, Day: 24}
	return PeriodsView{
		ProjectSlug: "home",
		HasCurrent:  true,
		HasClose:    true,
		Current:     PeriodRow{ID: "cur", Name: "November", Start: civil.Date{Year: 2026, Month: 10, Day: 25}, CanClose: true},
		History:     []PeriodRow{{ID: "old", Name: "October", Start: civil.Date{Year: 2026, Month: 9, Day: 25}, End: &end, Closed: true, CanReopen: true}},
	}
}

func TestPeriodsLayout_MemberSeesNoCloseOrReopen(t *testing.T) {
	d := web.LayoutData{ActiveOrg: &web.ActiveOrg{Slug: "acme"}}
	got := render(t, PeriodsLayout(d, periodsViewFixture()))

	requireContains(t, got, `data-period-read-only`, `>period.manager_only<`)
	for _, banned := range []string{`/periods/cur/close`, `/periods/old/reopen`, `close-month-dialog`, `period.close_hint`} {
		if strings.Contains(got, banned) {
			t.Errorf("member page contains %q", banned)
		}
	}
}

func TestPeriodsLayout_ManagerKeepsCloseAndReopen(t *testing.T) {
	d := web.LayoutData{ActiveOrg: &web.ActiveOrg{Slug: "acme", CanManage: true}}
	got := render(t, PeriodsLayout(d, periodsViewFixture()))

	requireContains(t, got, `data-dialog-open="close-month-dialog"`, `/periods/cur/close`, `/periods/old/reopen`, `period.close_hint`)
	if strings.Contains(got, `data-period-read-only`) {
		t.Error("manager page shows the managers-only line")
	}
}

func TestClosePeriodPage_HintsTheDefaultAndWarnsWhenEndingToday(t *testing.T) {
	d := web.LayoutData{ActiveOrg: &web.ActiveOrg{Slug: "acme"}}
	base := ClosePeriodView{ProjectSlug: "home", PeriodID: "cur", EndDate: "2026-11-17", MaxDate: "2026-11-18", HasPreview: true}

	got := render(t, ClosePeriodPage(d, base))
	requireContains(t, got, `period.end_default_hint`)
	if strings.Contains(got, `period.end_date_hint`) || strings.Contains(got, `period.end_today_hint`) {
		t.Errorf("yesterday's end shows a stale or today hint")
	}

	today := base
	today.EndDate = "2026-11-18"
	requireContains(t, render(t, ClosePreviewFragment(d, today)), `data-end-today`, `period.end_today_hint`)
}

func TestPeriodsLayout_CloseDialogWarnsWhenEndingToday(t *testing.T) {
	d := web.LayoutData{ActiveOrg: &web.ActiveOrg{Slug: "acme", CanManage: true}}
	v := periodsViewFixture()
	if strings.Contains(render(t, PeriodsLayout(d, v)), `data-end-today`) {
		t.Error("dialog warns about today for an earlier end")
	}
	v.CloseEndToday = true
	requireContains(t, render(t, PeriodsLayout(d, v)), `data-end-today`, `period.end_today_hint`)
}

func TestClosePeriodPage_EndErrorMarksTheEndDate(t *testing.T) {
	d := web.LayoutData{ActiveOrg: &web.ActiveOrg{Slug: "acme", CanManage: true}}
	v := ClosePeriodView{ProjectSlug: "home", PeriodID: "cur", EndDate: "2026-11-10", MaxDate: "2026-11-18", EndError: "Entries go up to 12 Nov 2026."}
	input := regexp.MustCompile(`<input[^>]*name="end_date" type="date"[^>]*>`).FindString(render(t, ClosePeriodPage(d, v)))

	requireContains(t, input, `value="2026-11-10"`, `aria-invalid="true"`, `aria-describedby="close-end-date-hint close-end-error"`)
	if n := strings.Count(input, "aria-describedby"); n != 1 {
		t.Errorf("end date carries %d aria-describedby attributes, want 1", n)
	}
	if n := strings.Count(render(t, ClosePeriodPage(d, v)), "Entries go up to 12 Nov 2026."); n != 1 {
		t.Errorf("end error rendered %d times, want 1", n)
	}
}

func TestClosePreviewFragment_CarriesTheEndError(t *testing.T) {
	d := web.LayoutData{ActiveOrg: &web.ActiveOrg{Slug: "acme", CanManage: true}}
	v := ClosePeriodView{ProjectSlug: "home", PeriodID: "cur", EndDate: "2026-11-10", MaxDate: "2026-11-18"}
	if strings.Contains(render(t, ClosePreviewFragment(d, v)), "close-end-error") {
		t.Error("clean preview carries an end error")
	}
	v.EndError = "Entries go up to 12 Nov 2026."
	requireContains(t, render(t, ClosePreviewFragment(d, v)), `<p id="close-end-error" data-end-error role="alert" class="text-sm text-error">Entries go up to 12 Nov 2026.</p>`)
}

func TestPeriodsLayout_SharedPeriodGetsGenericCloseCopy(t *testing.T) {
	d := web.LayoutData{ActiveOrg: &web.ActiveOrg{Slug: "acme", CanManage: true}}
	v := periodsViewFixture()
	v.Current.Label = PeriodLabel{Name: "Nov 2026", Start: civil.Date{Year: 2026, Month: 11, Day: 11}, Monthly: true, Shared: true}
	got := render(t, PeriodsLayout(d, v))
	requireContains(t, got, ">period.close_period<", "period.close_period_title")
	for _, banned := range []string{"overview.close_named", "period.close_title", ">period.close<"} {
		if strings.Contains(got, banned) {
			t.Errorf("shared period shows %s", banned)
		}
	}

	v.Current.Label = PeriodLabel{Name: "Nov 2026", Start: civil.Date{Year: 2026, Month: 11, Day: 1}, Monthly: true}
	requireContains(t, render(t, PeriodsLayout(d, v)), "overview.close_named", "period.close_title", ">period.close<")
}

func TestClosePeriodPage_SharedPeriodGetsGenericCloseCopy(t *testing.T) {
	d := web.LayoutData{ActiveOrg: &web.ActiveOrg{Slug: "acme", CanManage: true}}
	v := ClosePeriodView{ProjectSlug: "home", PeriodID: "cur", Label: PeriodLabel{Name: "Nov 2026", Start: civil.Date{Year: 2026, Month: 11, Day: 11}, Monthly: true, Shared: true}}
	got := render(t, ClosePeriodPage(d, v))
	if strings.Contains(got, ">period.close<") {
		t.Error("shared period's close page says close month")
	}
	requireContains(t, got, "period.close_period")
}

func TestPeriodsLayout_MemberCannotRename(t *testing.T) {
	member := render(t, PeriodsLayout(web.LayoutData{ActiveOrg: &web.ActiveOrg{Slug: "acme"}}, periodsViewFixture()))
	if strings.Contains(member, "/rename") {
		t.Error("member sees the rename form")
	}
	requireContains(t, render(t, PeriodsLayout(web.LayoutData{ActiveOrg: &web.ActiveOrg{Slug: "acme", CanManage: true}}, periodsViewFixture())), "/periods/cur/rename")
}

func TestPeriodHeadings_SayCloseMonthOnlyForAMonth(t *testing.T) {
	d := web.LayoutData{ActiveOrg: &web.ActiveOrg{Slug: "acme", CanManage: true}}
	from := PeriodLabel{Name: "Nov 2026", Start: civil.Date{Year: 2026, Month: 11, Day: 11}, Monthly: true, Shared: true}
	custom := PeriodLabel{Name: "Payday", Start: civil.Date{Year: 2026, Month: 11, Day: 25}}
	month := PeriodLabel{Name: "Nov 2026", Start: civil.Date{Year: 2026, Month: 11, Day: 1}, Monthly: true}
	for name, tc := range map[string]struct {
		label PeriodLabel
		want  string
	}{"from": {from, ">period.close_period<"}, "custom": {custom, ">period.close_period<"}, "month": {month, ">period.title<"}} {
		t.Run(name, func(t *testing.T) {
			v := periodsViewFixture()
			v.Current.Label = tc.label
			page := render(t, PeriodsPage(d, v))
			closePage := render(t, ClosePeriodPage(d, ClosePeriodView{ProjectSlug: "home", PeriodID: "cur", Label: tc.label}))
			if tc.want == ">period.title<" {
				requireContains(t, page, `tracking-tight text-balance">period.title</h1>`)
				requireContains(t, closePage, `btn-ghost">period.title</a>`)
				return
			}
			requireContains(t, page, `tracking-tight text-balance">period.close_period</h1>`)
			requireContains(t, closePage, `btn-ghost">period.close_period</a>`)
			for _, got := range []string{page, closePage} {
				if strings.Contains(got, ">period.title<") || strings.Contains(got, ">period.close<") {
					t.Error("non-month period shows a close-month heading")
				}
			}
		})
	}
}

func TestClosePeriodPage_EndErrorStylesAndFocusesTheEndDate(t *testing.T) {
	d := web.LayoutData{ActiveOrg: &web.ActiveOrg{Slug: "acme", CanManage: true}}
	page := regexp.MustCompile(`<form[^>]*class="group/close[^"]*"`)
	input := regexp.MustCompile(`<input[^>]*name="end_date" type="date"[^>]*>`)
	v := ClosePeriodView{ProjectSlug: "home", PeriodID: "cur", EndDate: "2026-11-10", MaxDate: "2026-11-18"}
	clean := render(t, ClosePeriodPage(d, v))
	requireContains(t, page.FindString(clean), "group/close")
	requireContains(t, input.FindString(clean), "group-has-[[data-end-error]]/close:input-error")
	if strings.Contains(input.FindString(clean), "autofocus") {
		t.Errorf("clean end date is focused")
	}
	v.EndError = "Entries go up to 12 Nov 2026."
	requireContains(t, input.FindString(render(t, ClosePeriodPage(d, v))), "autofocus")
}

func TestPeriodsPage_CloseHintSaysThisPeriodUnlessAMonth(t *testing.T) {
	d := web.LayoutData{ActiveOrg: &web.ActiveOrg{Slug: "acme", CanManage: true}}
	v := periodsViewFixture()
	v.Current.Label = PeriodLabel{Name: "Nov 2026", Start: civil.Date{Year: 2026, Month: 11, Day: 18}, Monthly: true, Shared: true}
	got := render(t, PeriodsPage(d, v))
	requireContains(t, got, "period.close_hint_this")
	if strings.Contains(got, ">period.close_hint<") {
		t.Error("a From period's hint names the period")
	}
	v.Current.Label = PeriodLabel{Name: "Nov 2026", Start: civil.Date{Year: 2026, Month: 11, Day: 1}, Monthly: true}
	requireContains(t, render(t, PeriodsPage(d, v)), ">period.close_hint<")
}
