package money_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/text/language"

	"altalune.id/yasaku/money"
)

func idID() language.Tag { return language.MustParse("id-ID") }
func enUS() language.Tag { return language.MustParse("en-US") }
func arSA() language.Tag { return language.MustParse("ar-SA") }
func jaJP() language.Tag { return language.MustParse("ja-JP") }
func msMY() language.Tag { return language.MustParse("ms-MY") }

func parseReason(t *testing.T, err error) string {
	t.Helper()
	pe, ok := errors.AsType[*money.ParseError](err)
	require.True(t, ok, "want *ParseError, got %v", err)
	return pe.Reason
}

func TestParseLocale(t *testing.T) {
	ok := []struct {
		in   string
		cur  money.Currency
		tag  language.Tag
		want int64
	}{
		{"12.500", money.IDR, idID(), 1250000},
		{"12.500", money.IDR, enUS(), 1250000},
		{"12,500", money.IDR, idID(), 1250000},
		{"12,500", money.IDR, enUS(), 1250000},
		{"12.500", "USD", idID(), 1250000},
		{"12,500", "USD", enUS(), 1250000},
		{"12.500,50", "USD", enUS(), 1250050},
		{"12.500,50", "USD", idID(), 1250050},
		{"12,500.50", "USD", enUS(), 1250050},
		{"12,500.50", "USD", idID(), 1250050},
		{"12,5", "USD", enUS(), 1250},
		{"12.34", "USD", idID(), 1234},
		{"1.250.000", money.IDR, idID(), 125000000},
		{"Rp 12.500", money.IDR, idID(), 1250000},
		{"$12,500.50", "USD", enUS(), 1250050},
		{"500", "JPY", jaJP(), 500},
		{"1,000", "JPY", jaJP(), 1000},
		{"12,500", "MYR", msMY(), 1250000},
		{"١٢٬٥٠٠", "USD", arSA(), 1250000},
		{"١٢٫٥٠", "USD", arSA(), 1250},
		{"۱۲۵۰۰", money.IDR, arSA(), 1250000},
		{"12 500", money.IDR, idID(), 1250000},
		{"12 500,50", "EUR", idID(), 1250050},
	}
	for _, tc := range ok {
		got, err := money.ParseLocale(tc.in, tc.cur, tc.tag)
		require.NoError(t, err, "%s %s %s", tc.in, tc.cur, tc.tag)
		require.Equal(t, tc.want, got.Minor, "%s %s %s", tc.in, tc.cur, tc.tag)
		require.Equal(t, tc.cur, got.Currency)
	}

	bad := []struct {
		in     string
		cur    money.Currency
		tag    language.Tag
		reason string
	}{
		{"12.500", "USD", enUS(), money.ReasonTooManyDecimals},
		{"12,500", "USD", idID(), money.ReasonTooManyDecimals},
		{"12.500,50", money.IDR, idID(), money.ReasonNoDecimals},
		{"12,500.50", money.IDR, enUS(), money.ReasonNoDecimals},
		{"0,500", "USD", enUS(), money.ReasonAmbiguous},
		{"", "USD", enUS(), money.ReasonEmpty},
		{"  ", money.IDR, idID(), money.ReasonEmpty},
		{"Rp", money.IDR, idID(), money.ReasonEmpty},
		{"-5", "USD", enUS(), money.ReasonNegative},
		{"−5", "USD", enUS(), money.ReasonNegative},
		{"12.5a", money.IDR, idID(), money.ReasonNotNumber},
		{"99999999999999999999", "USD", enUS(), money.ReasonOverflow},
		{"1+2", money.IDR, idID(), money.ReasonExpression},
		{"10*2", "USD", enUS(), money.ReasonExpression},
		{"10/2", "USD", enUS(), money.ReasonExpression},
		{"10×2", "USD", enUS(), money.ReasonExpression},
		{"10÷2", "USD", enUS(), money.ReasonExpression},
		{"10x2", "USD", enUS(), money.ReasonExpression},
		{"10X2", "USD", enUS(), money.ReasonExpression},
		{"(10)", "USD", enUS(), money.ReasonExpression},
		{"=10", "USD", enUS(), money.ReasonExpression},
		{"10-2", "USD", enUS(), money.ReasonExpression},
		{"10−2", "USD", enUS(), money.ReasonExpression},
		{"-10-2", "USD", enUS(), money.ReasonExpression},
	}
	for _, tc := range bad {
		got, err := money.ParseLocale(tc.in, tc.cur, tc.tag)
		require.Error(t, err, "%q %s %s parsed as %d", tc.in, tc.cur, tc.tag, got.Minor)
		require.Equal(t, tc.reason, parseReason(t, err), "%q %s %s", tc.in, tc.cur, tc.tag)
	}

	_, err := money.ParseLocale("5", "XXX", enUS())
	require.True(t, money.IsUnknownCurrencyError(err))
}

func TestParseMajorReasons(t *testing.T) {
	_, err := money.ParseMajor("12.345", "USD")
	require.Equal(t, money.ReasonAmbiguous, parseReason(t, err))
	_, err = money.ParseMajor("12,500.50", money.IDR)
	require.Equal(t, "currency has no decimals", parseReason(t, err))
	_, err = money.ParseMajor("1+2", money.IDR)
	require.Equal(t, money.ReasonExpression, parseReason(t, err))
}

func TestParseLocaleRoundTrip(t *testing.T) {
	currencies := []money.Currency{money.IDR, "USD", "SGD", "MYR", "EUR", "JPY"}
	tags := []language.Tag{idID(), enUS(), arSA(), jaJP(), msMY()}
	minors := []int64{0, 1, 5, 99, 100, 1234, 12500, 99999, 100000, 1250000, 123456789, 100000000000}
	for _, c := range currencies {
		step := int64(1)
		for range c.Exponent() - c.DisplayExponent() {
			step *= 10
		}
		for _, tag := range tags {
			for _, m := range minors {
				a := money.New(m*step, c)
				for _, s := range []string{a.Major(), a.Format(tag)} {
					got, err := money.ParseLocale(s, c, tag)
					require.NoError(t, err, "%s %s %q", c, tag, s)
					require.Equal(t, a, got, "%s %s %q", c, tag, s)
				}
			}
		}
	}
}
