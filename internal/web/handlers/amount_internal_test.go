package handlers

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/text/language"

	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/money"
)

func TestAmountErrorKey_EveryMoneyReasonHasATranslatedKey(t *testing.T) {
	reasons := map[string]string{
		money.ReasonEmpty:             "tx.amount_error.empty",
		money.ReasonNegative:          "tx.amount_error.negative",
		money.ReasonAmbiguous:         "tx.amount_error.ambiguous",
		money.ReasonNotNumber:         "tx.amount_error.not_number",
		money.ReasonTooManyDecimals:   "tx.amount_error.too_many_decimals",
		money.ReasonNoDecimals:        "tx.amount_error.no_decimals",
		money.ReasonExpression:        "tx.amount_error.expression",
		money.ReasonOverflow:          "tx.amount_error.overflow",
		transaction.ReasonNotPositive: "tx.amount_error.not_positive",
	}
	bundle := i18n.NewEmbeddedBundle(i18n.EnUS)
	for reason, want := range reasons {
		require.Equal(t, want, amountErrorKey(&transaction.InvalidAmountError{Reason: reason}), reason)
		for _, loc := range bundle.All() {
			require.NotEqual(t, want, bundle.For(loc).T(want), "%s untranslated in %s", want, loc)
		}
	}
	require.Empty(t, amountErrorKey(&transaction.InvalidAmountError{Reason: "unknown currency XXX"}))
	require.Empty(t, amountErrorKey(nil))
}

func TestParseAmount_KeepsTheEnglishReasonForTheRPCSide(t *testing.T) {
	_, err := parseAmount("12.500,50", money.IDR, language.MustParse("id-ID"))
	require.EqualError(t, err, "transaction: amount: "+money.ReasonNoDecimals)

	a, err := parseAmount("12.500", "USD", language.MustParse("id-ID"))
	require.NoError(t, err)
	require.Equal(t, int64(1250000), a.Minor)
}
