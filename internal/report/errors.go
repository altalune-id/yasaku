package report

import (
	"errors"
	"fmt"
)

var errPeriodAbsent = errors.New("report: period not found in scope")

var errTooManyPeriods = fmt.Errorf("report: cashflow accepts at most %d periods", maxCashflowPeriods)
