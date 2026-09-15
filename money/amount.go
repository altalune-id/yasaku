package money

import (
	"fmt"
	"strconv"
	"strings"
)

// Amount is a quantity of money in minor units.
type Amount struct {
	Minor    int64
	Currency Currency
}

// New builds an Amount.
func New(minor int64, c Currency) Amount { return Amount{Minor: minor, Currency: c} }

// Zero is the zero Amount of c.
func Zero(c Currency) Amount { return Amount{Currency: c} }

func (a Amount) mustSame(b Amount) {
	if a.Currency != b.Currency {
		panic(fmt.Sprintf("money: currency mismatch %s vs %s", a.Currency, b.Currency))
	}
}

// Add returns a+b; panics when currencies differ.
func (a Amount) Add(b Amount) Amount {
	a.mustSame(b)
	return Amount{Minor: a.Minor + b.Minor, Currency: a.Currency}
}

// Sub returns a-b; panics when currencies differ.
func (a Amount) Sub(b Amount) Amount {
	a.mustSame(b)
	return Amount{Minor: a.Minor - b.Minor, Currency: a.Currency}
}

// Neg returns -a.
func (a Amount) Neg() Amount { return Amount{Minor: -a.Minor, Currency: a.Currency} }

// Compare returns -1, 0 or 1; panics when currencies differ.
func (a Amount) Compare(b Amount) int {
	a.mustSame(b)
	switch {
	case a.Minor < b.Minor:
		return -1
	case a.Minor > b.Minor:
		return 1
	}
	return 0
}

// SameCurrency reports whether a and b share a currency.
func (a Amount) SameCurrency(b Amount) bool { return a.Currency == b.Currency }

// IsZero reports whether Minor is zero.
func (a Amount) IsZero() bool { return a.Minor == 0 }

// IsPositive reports whether Minor is greater than zero.
func (a Amount) IsPositive() bool { return a.Minor > 0 }

// Major renders a in major units with the currency's display decimals, or full precision when minor digits are present.
func (a Amount) Major() string {
	exp := a.Currency.Exponent()
	if exp == 0 {
		return strconv.FormatInt(a.Minor, 10)
	}
	neg := a.Minor < 0
	abs := a.Minor
	if neg {
		abs = -abs
	}
	pow := pow10(exp)
	whole, frac := abs/pow, abs%pow
	s := strconv.FormatInt(whole, 10)
	if frac != 0 || a.Currency.DisplayExponent() > 0 {
		fs := strconv.FormatInt(frac, 10)
		s += "." + strings.Repeat("0", exp-len(fs)) + fs
	}
	if neg {
		s = "-" + s
	}
	return s
}

// String renders "CUR minor" for logs.
func (a Amount) String() string { return fmt.Sprintf("%s %d", a.Currency, a.Minor) }

func pow10(n int) int64 {
	p := int64(1)
	for range n {
		p *= 10
	}
	return p
}
