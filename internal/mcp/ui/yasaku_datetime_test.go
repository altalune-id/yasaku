package ui

import "testing"

func TestDateTimeFallsBackToUTCWithoutIntl(t *testing.T) {
	vm := newJSVM(t)
	for expr, want := range map[string]string{
		`dateTime("2026-09-22T08:00:00Z")`:         "2026-09-22 08:00 UTC",
		`dateTime("2026-09-22T08:00:00.123456Z")`:  "2026-09-22 08:00 UTC",
		`dateTime("2026-09-22")`:                   "—",
		`dateTime(undefined)`:                      "—",
		`dateTime({__raw:"2026-09-22T08:00:00Z"})`: "—",
		`recordedTitle("2026-09-22T08:00:00Z")`:    "",
		`recordedTitle(undefined)`:                 "",
	} {
		if got := jsString(t, vm, expr); got != want {
			t.Errorf("%s = %q, want %q", expr, got, want)
		}
	}
}

func TestTxRowTitleNeedsIntlAndPeriodRowCarriesClosedAt(t *testing.T) {
	vm := newJSVM(t)
	for expr, want := range map[string]string{
		`txRowModel({kind:"expense",date:"2026-09-23",occurredAt:"2026-09-22T19:30:00Z"}).date`:  "2026-09-23",
		`txRowModel({kind:"expense",date:"2026-09-23",occurredAt:"2026-09-22T19:30:00Z"}).title`: "",
		`txRowModel({kind:"expense",date:"2026-09-23"}).title`:                                   "",
		`periodRowModel({name:"Aug",status:"closed",closedAt:"2026-09-01T00:00:00Z"}).closedAt`:  "2026-09-01 00:00 UTC",
		`periodRowModel({name:"Sep",status:"open"}).closedAt`:                                    "",
	} {
		if got := jsString(t, vm, expr); got != want {
			t.Errorf("%s = %q, want %q", expr, got, want)
		}
	}
}
