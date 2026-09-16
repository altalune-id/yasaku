package templates

import (
	"testing"

	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/money"
)

func TestMoney(t *testing.T) {
	tests := []struct {
		name   string
		locale string
		amount money.Amount
		want   string
	}{
		{"idr en-US", "en-US", money.New(4000000, money.IDR), "Rp40,000"},
		{"idr id-ID", "id-ID", money.New(4000000, money.IDR), "Rp40.000"},
		{"usd id-ID", "id-ID", money.New(123456, "USD"), "$1.234,56"},
		{"empty locale falls back", "", money.New(4000000, money.IDR), "Rp40,000"},
		{"unparseable locale falls back", "not-a-locale", money.New(4000000, money.IDR), "Rp40,000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := web.LayoutData{Locale: i18n.Locale(tt.locale)}
			if got := Money(d, tt.amount); got != tt.want {
				t.Fatalf("Money() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSignedMoney(t *testing.T) {
	tests := []struct {
		name   string
		amount money.Amount
		want   string
	}{
		{"inflow gets a plus", money.New(4000000, money.IDR), "+Rp40.000"},
		{"outflow gets U+2212", money.New(-4000000, money.IDR), "−Rp40.000"},
		{"zero stays bare", money.Zero(money.IDR), "Rp0"},
	}
	d := web.LayoutData{Locale: i18n.IdID}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SignedMoney(d, tt.amount); got != tt.want {
				t.Fatalf("SignedMoney() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSignedMoney_UsesRealMinusNotHyphen(t *testing.T) {
	d := web.LayoutData{Locale: i18n.EnUS}
	got := SignedMoney(d, money.New(-100, "USD"))
	if got[0] == '-' {
		t.Fatalf("SignedMoney() = %q, must use U+2212 MINUS SIGN, not a hyphen", got)
	}
	if want := "−$1.00"; got != want {
		t.Fatalf("SignedMoney() = %q, want %q", got, want)
	}
}
