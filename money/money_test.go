package money_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/text/language"

	"altalune.id/yasaku/money"
)

func TestCurrency(t *testing.T) {
	c, err := money.ParseCurrency("idr")
	require.NoError(t, err)
	require.Equal(t, money.IDR, c)
	require.Equal(t, 2, c.Exponent())
	require.Equal(t, 0, c.DisplayExponent())
	_, err = money.ParseCurrency("XXX")
	require.True(t, money.IsUnknownCurrencyError(err))
	require.False(t, money.Currency("").Valid())
}

func TestParseMajor(t *testing.T) {
	cases := []struct {
		in   string
		cur  money.Currency
		want int64
	}{
		{"40000", money.IDR, 4000000},
		{"40.000", money.IDR, 4000000},
		{"40,000", money.IDR, 4000000},
		{"Rp 1.250.000", money.IDR, 125000000},
		{"12.34", "USD", 1234},
		{"1,234.56", "USD", 123456},
		{"1.234,56", "USD", 123456},
		{"12,345", "USD", 1234500},
		{"500", "JPY", 500},
		{"12.500,00", money.IDR, 1250000},
		{"12,500.00", money.IDR, 1250000},
		{"12500,0", money.IDR, 1250000},
		{"12.500", money.IDR, 1250000},
		{"12,500", money.IDR, 1250000},
		{"12500", money.IDR, 1250000},
		{"1.250.000", money.IDR, 125000000},
		{"12.500,50", "USD", 1250050},
		{"12,500.50", "USD", 1250050},
		{"12500.5", "USD", 1250050},
		{"500,00", "JPY", 500},
		{"1,250.000", money.IDR, 125000},
		{"12.500,000", money.IDR, 1250000},
		{"12500.", money.IDR, 1250000},
		{"1,000", "JPY", 1000},
		{"100.00", "JPY", 100},
	}
	for _, tc := range cases {
		got, err := money.ParseMajor(tc.in, tc.cur)
		require.NoError(t, err, tc.in)
		require.Equal(t, tc.want, got.Minor, tc.in)
		require.Equal(t, tc.cur, got.Currency)
	}
	for _, bad := range []string{"", "abc", "12.345", "-5"} {
		_, err := money.ParseMajor(bad, "USD")
		require.True(t, money.IsParseError(err), bad)
	}
	for _, bad := range []string{"12.500,50", "12,500.50", "12500,5", "1.5", "12.500,05", "Rp 12.500,50"} {
		got, err := money.ParseMajor(bad, money.IDR)
		require.True(t, money.IsParseError(err), "%s parsed as %d", bad, got.Minor)
		require.Contains(t, err.Error(), "currency has no decimals", bad)
	}
	for _, bad := range []string{"1.2.345", "12.50.000", "1,25,000", ",000", "1.000.0", "12.500.", "1.000,000.0", "0.500", "00.500", "0,500"} {
		got, err := money.ParseMajor(bad, money.IDR)
		require.True(t, money.IsParseError(err), "%s parsed as %d", bad, got.Minor)
	}
	_, err := money.ParseMajor("12.5a", money.IDR)
	require.ErrorContains(t, err, "not a number")
	for _, bad := range []string{"500,50", "1.5"} {
		_, err := money.ParseMajor(bad, "JPY")
		require.True(t, money.IsParseError(err), bad)
	}
}

func TestArithmeticAndFormat(t *testing.T) {
	a := money.New(4000000, money.IDR)
	b := money.New(150000, money.IDR)
	require.Equal(t, int64(4150000), a.Add(b).Minor)
	require.Equal(t, int64(3850000), a.Sub(b).Minor)
	require.Equal(t, 1, a.Compare(b))
	require.True(t, a.IsPositive())
	require.True(t, money.Zero(money.IDR).IsZero())
	require.Panics(t, func() { a.Add(money.New(1, "USD")) })
	require.Equal(t, "40000", a.Major())
	require.Equal(t, "40000.50", money.New(4000050, money.IDR).Major())
	require.Equal(t, "12.34", money.New(1234, "USD").Major())
	require.Equal(t, "Rp40.000", a.Format(language.Indonesian))
	require.Equal(t, "Rp40,000", a.Format(language.AmericanEnglish))
	require.Equal(t, "$12.34", money.New(1234, "USD").Format(language.AmericanEnglish))
	require.Equal(t, "IDR 4000000", a.String())
	require.Equal(t, "0", money.Amount{}.Format(language.AmericanEnglish))
}
