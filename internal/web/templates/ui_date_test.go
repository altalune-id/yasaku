package templates

import (
	"strings"
	"testing"

	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/web"
)

func TestDateFieldKeepsANativeInputAndAGregorianCalendar(t *testing.T) {
	t.Parallel()
	d := web.LayoutData{Nonce: "n0nce", Locale: i18n.Locale("ar-SA")}
	got := renderKit(t, DateField(d, DateFieldProps{
		ID: "tx-date", Name: "date", Label: "Date", Value: "2026-11-10", Today: "2026-11-17", Yesterday: "2026-11-16",
	}), "")

	requireContainsAll(t, got,
		`<input id="tx-date" name="date" type="date" value="2026-11-10" required data-date-input`,
		`locale="ar-SA-u-ca-gregory"`,
		`first-day-of-week="0"`,
		`class="cally`,
		`[&::part(day)]:size-11`,
		`data-date-set="2026-11-17"`,
		`data-date-set="2026-11-16"`,
		`data-dialog-open="tx-date-dialog"`,
		`nonce="n0nce" type="module"`,
	)
	if !strings.Contains(got, `<div data-date-picker hidden`) {
		t.Fatalf("the calendar chip must stay hidden until datepicker.js mounts: %q", got)
	}
	if strings.Contains(got, `<div data-date-native hidden`) {
		t.Fatalf("without JS the native input must stay visible: %q", got)
	}
}

func TestWeekStartFollowsTheLocaleRegion(t *testing.T) {
	t.Parallel()
	for locale, want := range map[string]int{"en-US": 0, "id-ID": 0, "ja-JP": 0, "ar-SA": 0, "ms-MY": 1, "en-GB": 1, "ar-EG": 6} {
		if got := weekStart(locale); got != want {
			t.Errorf("weekStart(%q) = %d, want %d", locale, got, want)
		}
	}
}
