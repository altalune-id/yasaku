package money

import (
	"errors"
	"fmt"
)

// UnknownCurrencyError reports a code outside the supported table.
type UnknownCurrencyError struct{ Code string }

func (e *UnknownCurrencyError) Error() string {
	return fmt.Sprintf("money: unknown currency %q", e.Code)
}

// IsUnknownCurrencyError reports whether err's chain contains an *UnknownCurrencyError.
func IsUnknownCurrencyError(err error) bool {
	_, ok := errors.AsType[*UnknownCurrencyError](err)
	return ok
}

// ParseError reports an amount string that could not be read.
type ParseError struct{ Input, Reason string }

func (e *ParseError) Error() string { return fmt.Sprintf("money: parse %q: %s", e.Input, e.Reason) }

// IsParseError reports whether err's chain contains a *ParseError.
func IsParseError(err error) bool {
	_, ok := errors.AsType[*ParseError](err)
	return ok
}
