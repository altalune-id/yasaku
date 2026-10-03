package templates

import (
	"strings"
	"testing"
	"time"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/web"
)

func jakarta(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Fatalf("load Asia/Jakarta: %v", err)
	}
	return loc
}

func requireContains(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Fatalf("got %q, want it to contain %q", got, want)
		}
	}
}

func TestDateTime_RendersUTCInstantWithLedgerFallback(t *testing.T) {
	at := time.Date(2026, 10, 2, 6, 30, 0, 0, time.FixedZone("CEST", 2*3600))
	got := render(t, DateTime(web.LayoutData{TimeZone: jakarta(t)}, at))

	requireContains(t, got,
		`<time datetime="2026-10-02T04:30:00Z"`,
		`data-datetime`,
		`data-ledger-tz="Asia/Jakarta"`,
		`>2 Oct 2026 11:30 WIB</time>`,
		`data-dt-alt-label="datetime.ledger_time"`,
	)
	requirePlain(t, got)
	if inlineOn.MatchString(got) {
		t.Fatalf("DateTime() must carry no inline handler: %q", got)
	}
}

func TestDateTime_WithoutLedgerFallsBackToUTC(t *testing.T) {
	at := time.Date(2026, 10, 2, 6, 30, 0, 0, time.FixedZone("CEST", 2*3600))
	got := render(t, DateTime(web.LayoutData{}, at))

	requireContains(t, got, `<time datetime="2026-10-02T04:30:00Z"`, `>2 Oct 2026 04:30 UTC</time>`)
	if strings.Contains(got, "data-ledger-tz") {
		t.Fatalf("DateTime() without a ledger must render no ledger zone: %q", got)
	}
	requirePlain(t, got)
}

func TestLedgerDate_RendersTheCivilDateAndTheMoment(t *testing.T) {
	day := civil.Date{Year: 2026, Month: time.October, Day: 1}
	at := time.Date(2026, 10, 1, 5, 0, 0, 0, jakarta(t))
	got := render(t, LedgerDate(web.LayoutData{TimeZone: jakarta(t)}, day, &at))

	requireContains(t, got,
		`<time datetime="2026-10-01"`,
		`data-ledger-date`,
		`>1 Oct 2026</time>`,
		`data-datetime-alt="2026-09-30T22:00:00Z"`,
		`data-ledger-tz="Asia/Jakarta"`,
		`data-dt-alt-label="datetime.recorded"`,
	)
	requirePlain(t, got)
}

func requirePlain(t *testing.T, got string) {
	t.Helper()
	for _, banned := range []string{"<button", " popover", " disabled", "z-10"} {
		if strings.Contains(got, banned) {
			t.Fatalf("server markup must be a plain <time> so a click falls through to the row link; found %q in %q", banned, got)
		}
	}
}

func TestLedgerDate_WithoutMomentIsPlainText(t *testing.T) {
	day := civil.Date{Year: 2026, Month: time.October, Day: 1}
	got := render(t, LedgerDate(web.LayoutData{TimeZone: jakarta(t)}, day, nil))

	requireContains(t, got, `<time datetime="2026-10-01"`, `>1 Oct 2026</time>`)
	if strings.Contains(got, "data-datetime-alt") || strings.Contains(got, "<button") {
		t.Fatalf("LedgerDate() without a moment must render no popover: %q", got)
	}
}

func TestLedgerDate_CompactDropsTheYear(t *testing.T) {
	day := civil.Date{Year: 2026, Month: time.October, Day: 1}
	got := render(t, LedgerDate(web.LayoutData{}, day, nil, Compact()))

	requireContains(t, got, `data-date-style="compact"`, `>1 Oct</time>`)
}

func TestTimeZoneHints_RenderHiddenWithTheLedgerZone(t *testing.T) {
	d := web.LayoutData{TimeZone: jakarta(t), Nonce: "n0nce"}

	footer := render(t, LedgerTimeFooter(d))
	requireContains(t, footer, `data-tz-hint`, ` hidden`, `data-ledger-tz="Asia/Jakarta"`, `datetime.footer_hint`,
		`src="/static/localtime.js`, `nonce="n0nce"`, ` defer`,
		`<template data-dt-template>`, `data-dt-trigger`, `data-dt-pop`, ` popover`)

	input := render(t, DateInputHint(d))
	requireContains(t, input, `data-tz-hint`, ` hidden`, `data-ledger-tz="Asia/Jakarta"`, `datetime.input_hint`)
}

func TestTimeZoneHints_WithoutLedgerStillLoadTheScript(t *testing.T) {
	footer := render(t, LedgerTimeFooter(web.LayoutData{Nonce: "n0nce"}))
	requireContains(t, footer, `src="/static/localtime.js`)
	if strings.Contains(footer, "data-tz-hint") {
		t.Fatalf("no ledger zone, no hint: %q", footer)
	}
	if got := render(t, DateInputHint(web.LayoutData{})); strings.TrimSpace(got) != "" {
		t.Fatalf("DateInputHint() without a ledger = %q, want empty", got)
	}
}
