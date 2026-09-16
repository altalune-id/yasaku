package report

import (
	"errors"
	"fmt"
)

// errPeriodAbsent reports a period id that does not resolve inside the caller's scope; callers
// resolve the period before reporting on it, so this travels as an unexpected error, not a domain one.
var errPeriodAbsent = errors.New("report: period not found in scope")

var errTooManyPeriods = fmt.Errorf("report: cashflow accepts at most %d periods", maxCashflowPeriods)
