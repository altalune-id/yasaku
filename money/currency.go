// Package money holds integer minor-unit amounts and currency metadata.
package money

import "strings"

// Currency is an ISO 4217 code.
type Currency string

// IDR is the Indonesian rupiah, the project default.
const IDR Currency = "IDR"

type meta struct {
	exponent, display int
	symbol            string
}

var currencies = map[Currency]meta{ //nolint:gochecknoglobals // immutable table
	IDR:   {2, 0, "Rp"},
	"USD": {2, 2, "$"},
	"SGD": {2, 2, "S$"},
	"MYR": {2, 2, "RM"},
	"EUR": {2, 2, "€"},
	"JPY": {0, 0, "¥"},
}

// ParseCurrency validates code case-insensitively.
func ParseCurrency(code string) (Currency, error) {
	c := Currency(strings.ToUpper(strings.TrimSpace(code)))
	if !c.Valid() {
		return "", &UnknownCurrencyError{Code: code}
	}
	return c, nil
}

// Valid reports whether c is a supported currency.
func (c Currency) Valid() bool { _, ok := currencies[c]; return ok }

// Exponent is the number of minor-unit digits stored per major unit.
func (c Currency) Exponent() int { return currencies[c].exponent }

// DisplayExponent is the number of decimals shown to users.
func (c Currency) DisplayExponent() int { return currencies[c].display }

func (c Currency) symbol() string { return currencies[c].symbol }
