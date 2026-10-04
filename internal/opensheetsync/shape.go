package opensheetsync

import (
	"cmp"
	"errors"
	"net/http"
	"slices"

	"altalune.id/yasaku/httpclient"
	"altalune.id/yasaku/opensheet"
)

// TabCheck is one tab's Test result: what opensheet reported and the first rule it broke.
type TabCheck struct {
	Entity    Entity
	Sheet     string
	Reachable bool
	IDColumn  bool
	Writable  bool
	// ColumnsDeferred reports that opensheet showed no columns for the tab yet, so the first sync checks them.
	ColumnsDeferred bool
	Missing         []string
	ContractReason  string
	Err             error
}

// OK reports whether the tab passed every check.
func (c TabCheck) OK() bool { return c.Err == nil }

// Checklist is the Test result for every contract tab, in tutorial order.
type Checklist []TabCheck

// OK reports whether every tab passed; an empty checklist never passes.
func (l Checklist) OK() bool {
	if len(l) == 0 {
		return false
	}
	return !slices.ContainsFunc(l, func(c TabCheck) bool { return c.Err != nil })
}

// Err returns the first failing tab's typed error, or nil; the checklist carries the rest. NOTE: not errors.Join, because apperror.AsAppError walks a single Unwrap chain.
func (l Checklist) Err() error {
	for _, c := range l {
		if c.Err != nil {
			return c.Err
		}
	}
	return nil
}

// CheckTab judges one sheet's capabilities, or the error fetching them, against tab.
func CheckTab(tab Tab, sheet string, caps opensheet.SheetCapabilities, err error) TabCheck {
	c := TabCheck{Entity: tab.Entity, Sheet: sheet}
	if err != nil {
		c.Err = fetchRefusal(sheet, err)
		return c
	}
	c.Reachable, c.Writable = true, caps.Writable
	c.ColumnsDeferred = !columnsObservable(caps.Columns)
	verdict := caps.SatisfiesContract && caps.ValidatedAt != nil
	c.IDColumn = caps.IDColumn || (c.ColumnsDeferred && verdict)
	c.Missing = missingColumns(tab, caps, c.ColumnsDeferred)
	switch {
	case !caps.SatisfiesContract:
		c.ContractReason = cmp.Or(caps.ContractReason, "the sheet does not satisfy opensheet's table contract")
	case c.ColumnsDeferred && !verdict:
		c.ContractReason = "opensheet has not read and validated this tab yet; open its rows in opensheet once, then run Test again"
	}
	switch {
	case len(c.Missing) > 0:
		c.Err = &ShapeMismatchError{Sheet: sheet, Missing: slices.Clone(c.Missing)}
	case !c.IDColumn && !c.ColumnsDeferred:
		c.Err = &NoIDColumnError{Sheet: sheet}
	case !c.Writable:
		c.Err = &SheetNotWritableError{Sheet: sheet}
	case c.ContractReason != "":
		c.Err = &ContractUnsatisfiedError{Sheet: sheet, Reason: c.ContractReason}
	}
	return c
}

func fetchRefusal(sheet string, err error) error {
	if httpclient.IsPrivateAddressError(err) {
		return &PrivateEndpointError{Cause: err}
	}
	if opensheet.IsNotFoundError(err) {
		return &SheetUnreachableError{Sheet: sheet}
	}
	if ae, ok := errors.AsType[*opensheet.APIError](err); ok && (ae.Status == http.StatusUnauthorized || ae.Status == http.StatusForbidden) {
		return &SheetUnreachableError{Sheet: sheet}
	}
	if ve, ok := errors.AsType[*opensheet.ValidationError](err); ok {
		return &ContractUnsatisfiedError{Sheet: sheet, Reason: cmp.Or(ve.Message, ve.Code)}
	}
	return &UnavailableError{Sheet: sheet, Cause: err.Error()}
}

// NOTE: columns come from opensheet's saved header row, but an unread or pre-upgrade sheet falls back to the first live row, so an empty one shows nothing to check and the deferral stays.
func columnsObservable(columns []string) bool {
	return slices.ContainsFunc(columns, func(c string) bool { return c != deletedAtColumn })
}

func missingColumns(tab Tab, caps opensheet.SheetCapabilities, deferred bool) []string {
	if deferred {
		if caps.SoftDelete {
			return nil
		}
		return []string{deletedAtColumn}
	}
	var out []string
	for _, col := range tab.Columns {
		if !slices.Contains(caps.Columns, col) {
			out = append(out, col)
		}
	}
	return out
}
