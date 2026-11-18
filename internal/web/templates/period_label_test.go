package templates

import (
	"testing"
	"time"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/web"
)

func TestPeriodLabel_TextAndMarkupFollowTheRule(t *testing.T) {
	nov1 := civil.Date{Year: 2026, Month: time.November, Day: 1}
	nov17 := civil.Date{Year: 2026, Month: time.November, Day: 17}
	nov18 := civil.Date{Year: 2026, Month: time.November, Day: 18}
	dec25 := civil.Date{Year: 2026, Month: time.December, Day: 25}
	jan24 := civil.Date{Year: 2027, Month: time.January, Day: 24}
	d := web.LayoutData{Translator: i18n.NewEmbeddedBundle(i18n.EnUS).For(i18n.EnUS)}

	cases := []struct {
		name   string
		label  PeriodLabel
		text   string
		markup []string
	}{
		{"custom name", PeriodLabel{Name: "Holiday <fund>", Start: nov1}, "Holiday <fund>", []string{"Holiday &lt;fund&gt;"}},
		{"auto month", PeriodLabel{Name: "Nov 2026", Start: nov1, Monthly: true}, "November 2026", []string{`datetime="2026-11-01" data-ledger-date data-date-style="month">November 2026</time>`}},
		{"shared closed", PeriodLabel{Name: "Nov 2026", Start: nov1, End: &nov17, Monthly: true, Shared: true}, "Nov 1 – Nov 17, 2026",
			[]string{`data-date-style="compact">1 Nov</time> – <time datetime="2026-11-17"`, `data-date-style="medium">17 Nov 2026</time>`}},
		{"shared closed across years", PeriodLabel{Name: "Dec 2026", Start: dec25, End: &jan24, Monthly: true, Shared: true}, "Dec 25, 2026 – Jan 24, 2027",
			[]string{`data-date-style="medium">25 Dec 2026</time> – `}},
		{"shared open", PeriodLabel{Name: "Nov 2026", Start: nov18, Monthly: true, Shared: true}, "From Nov 18, 2026", []string{`From <time datetime="2026-11-18" data-ledger-date data-date-style="medium">18 Nov 2026</time>`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PeriodLabelText(d, tc.label); got != tc.text {
				t.Errorf("text = %q, want %q", got, tc.text)
			}
			requireContains(t, render(t, PeriodLabelView(d, tc.label)), tc.markup...)
		})
	}
}

func TestDateText_FollowsTheLocale(t *testing.T) {
	day := civil.Date{Year: 2026, Month: time.November, Day: 11}
	start := civil.Date{Year: 2026, Month: time.November, Day: 1}
	end := civil.Date{Year: 2026, Month: time.November, Day: 17}
	shared := PeriodLabel{Name: "Nov 2026", Start: start, End: &end, Monthly: true, Shared: true}
	month := PeriodLabel{Name: "Nov 2026", Start: start, Monthly: true}
	cases := []struct {
		loc                i18n.Locale
		date, rng, monthly string
	}{
		{i18n.EnUS, "Nov 11, 2026", "Nov 1 – Nov 17, 2026", "November 2026"},
		{i18n.ArSA, "11 نوفمبر 2026", "من 1 نوفمبر إلى 17 نوفمبر 2026", "نوفمبر 2026"},
		{i18n.JaJP, "2026年11月11日", "11月1日～2026年11月17日", "2026年11月"},
		{i18n.IdID, "11 Nov 2026", "1 Nov – 17 Nov 2026", "November 2026"},
		{i18n.MsMY, "11 Nov 2026", "1 Nov – 17 Nov 2026", "November 2026"},
	}
	for _, tc := range cases {
		t.Run(string(tc.loc), func(t *testing.T) {
			d := web.LayoutData{Translator: i18n.NewEmbeddedBundle(tc.loc).For(tc.loc)}
			if got := LedgerDateText(d, day); got != tc.date {
				t.Errorf("date = %q, want %q", got, tc.date)
			}
			if got := PeriodLabelText(d, shared); got != tc.rng {
				t.Errorf("range = %q, want %q", got, tc.rng)
			}
			if got := PeriodLabelText(d, month); got != tc.monthly {
				t.Errorf("month = %q, want %q", got, tc.monthly)
			}
		})
	}
}
