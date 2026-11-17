package handlers

import (
	"errors"

	"golang.org/x/text/language"

	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/money"
)

//nolint:gochecknoglobals // immutable lookup table
var amountErrorKeys = map[string]string{
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

func parseAmount(raw string, cur money.Currency, tag language.Tag) (money.Amount, error) {
	a, err := money.ParseLocale(raw, cur, tag)
	if err == nil {
		return a, nil
	}
	if pe, ok := errors.AsType[*money.ParseError](err); ok {
		return money.Amount{}, &transaction.InvalidAmountError{Reason: pe.Reason}
	}
	return money.Amount{}, err
}

//i18n:use tx.amount_error.*
func amountErrorKey(err error) string {
	ie, ok := errors.AsType[*transaction.InvalidAmountError](err)
	if !ok {
		return ""
	}
	return amountErrorKeys[ie.Reason]
}
