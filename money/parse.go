package money

import (
	"strconv"
	"strings"
)

// ParseMajor parses a user-typed major-unit amount for c. NOTE: for a currency shown with decimals, a trailing "," that isn't a valid decimal mark falls back to grouping, but a trailing "." in that position is an error.
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
	whole, frac, reason := splitDecimal(raw, c.DisplayExponent())
	if reason != "" {
		return Amount{}, &ParseError{Input: s, Reason: reason}
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

func splitDecimal(raw string, displayExp int) (whole, frac, reason string) {
	i := strings.LastIndexAny(raw, ".,")
	if i < 0 {
		return raw, "", ""
	}
	sep := raw[i]
	tail := raw[i+1:]
	if displayExp == 0 {
		return splitNoDecimals(raw, i, tail)
	}
	if len(tail) >= 1 && len(tail) <= displayExp {
		return raw[:i], tail, ""
	}
	if sep == ',' {
		return raw, "", ""
	}
	return "", "", "ambiguous decimal separator"
}

// NOTE: a lone three-digit tail is grouping; any other tail is a decimal part, dropped only when it is all zeros.
func splitNoDecimals(raw string, i int, tail string) (whole, frac, reason string) {
	if strings.Trim(tail, "0123456789") != "" {
		return raw, "", ""
	}
	mark, head := raw[i], raw[:i]
	other := byte('.')
	if mark == '.' {
		other = ','
	}
	if len(tail) == 3 && strings.IndexByte(head, other) < 0 {
		if !validGroups(raw, mark) {
			return "", "", "ambiguous decimal separator"
		}
		return raw, "", ""
	}
	if strings.IndexByte(head, mark) >= 0 || !validGroups(head, other) {
		return "", "", "ambiguous decimal separator"
	}
	if strings.Trim(tail, "0") != "" {
		return "", "", "currency has no decimals"
	}
	return head, "", ""
}

func validGroups(s string, sep byte) bool {
	groups := strings.Split(s, string(sep))
	if len(groups) == 1 {
		return true
	}
	if len(groups[0]) < 1 || len(groups[0]) > 3 || groups[0][0] == '0' {
		return false
	}
	for _, g := range groups[1:] {
		if len(g) != 3 {
			return false
		}
	}
	return true
}
