package money

import (
	"strconv"
	"strings"
)

// ParseMajor parses a user-typed major-unit amount for c. NOTE: a trailing "," that isn't a valid decimal mark falls back to grouping, but a trailing "." in that position is an error.
func ParseMajor(s string, c Currency) (Amount, error) {
	if !c.Valid() {
		return Amount{}, &UnknownCurrencyError{Code: string(c)}
	}
	raw := strings.TrimSpace(s)
	raw = strings.TrimPrefix(raw, c.symbol())
	raw = strings.TrimPrefix(strings.ToUpper(raw), string(c))
	raw = strings.ReplaceAll(strings.TrimSpace(raw), " ", "")
	if raw == "" {
		return Amount{}, &ParseError{Input: s, Reason: "empty"}
	}
	if strings.HasPrefix(raw, "-") {
		return Amount{}, &ParseError{Input: s, Reason: "negative"}
	}
	whole, frac, ok := splitDecimal(raw, c.DisplayExponent())
	if !ok {
		return Amount{}, &ParseError{Input: s, Reason: "ambiguous decimal separator"}
	}
	whole = strings.NewReplacer(".", "", ",", "").Replace(whole)
	if whole == "" || strings.Trim(whole, "0123456789") != "" || strings.Trim(frac, "0123456789") != "" {
		return Amount{}, &ParseError{Input: s, Reason: "not a number"}
	}
	exp := c.Exponent()
	if len(frac) > exp {
		return Amount{}, &ParseError{Input: s, Reason: "too many decimals"}
	}
	frac += strings.Repeat("0", exp-len(frac))
	n, err := strconv.ParseInt(whole+frac, 10, 64)
	if err != nil {
		return Amount{}, &ParseError{Input: s, Reason: "overflow"}
	}
	return Amount{Minor: n, Currency: c}, nil
}

func splitDecimal(raw string, displayExp int) (whole, frac string, ok bool) {
	if displayExp == 0 {
		return raw, "", true
	}
	i := strings.LastIndexAny(raw, ".,")
	if i < 0 {
		return raw, "", true
	}
	sep := raw[i]
	tail := raw[i+1:]
	if len(tail) >= 1 && len(tail) <= displayExp {
		return raw[:i], tail, true
	}
	if sep == ',' {
		return raw, "", true
	}
	return "", "", false
}
