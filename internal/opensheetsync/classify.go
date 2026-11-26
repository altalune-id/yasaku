package opensheetsync

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"altalune.id/yasaku/httpclient"
	"altalune.id/yasaku/opensheet"
)

const (
	codeNoIDColumn            = "SHT010"
	codeAmbiguousIDColumn     = "SHT011"
	codeDuplicateID           = "SHT012"
	codeUnknownColumn         = "SHT014"
	codeWriteInFlight         = "SHT017"
	codeIdempotencyMismatch   = "SHT018"
	codeDuplicateColumn       = "SHT021"
	codeContractViolation     = "SHT023"
	codeSoftDeleteUnsupported = "SHT029"
)

//nolint:gochecknoglobals // a compiled pattern, not runtime state.
var storedCodeRe = regexp.MustCompile(`^[A-Z]{3}\d{3}$`)

// ErrorCode reads the code back out of a stored last_error in the shapes refusalText and failureText write: the token after the first ": ", else a trailing "(CODE)"; "" when it carries none.
func ErrorCode(lastError string) string {
	if _, rest, ok := strings.Cut(lastError, ": "); ok {
		if tok, _, _ := strings.Cut(rest, " "); storedCodeRe.MatchString(tok) {
			return tok
		}
	}
	if !strings.HasSuffix(lastError, ")") {
		return ""
	}
	i := strings.LastIndex(lastError, "(")
	if i < 0 {
		return ""
	}
	if tok := lastError[i+1 : len(lastError)-1]; storedCodeRe.MatchString(tok) {
		return tok
	}
	return ""
}

// Reason is what a stored opensheet code means for the person fixing the link.
type Reason string

// Reasons a stored refusal can carry.
const (
	ReasonOther           Reason = "other"
	ReasonKeyRejected     Reason = "key_rejected"
	ReasonSheetReadOnly   Reason = "sheet_read_only"
	ReasonSheetMissing    Reason = "sheet_missing"
	ReasonGoogleAccess    Reason = "google_access"
	ReasonKeyUnreadable   Reason = "key_unreadable"
	ReasonPrivateEndpoint Reason = "private_endpoint"
)

// ReasonOf maps an opensheet or yasaku error code to its Reason; an unknown or empty code is ReasonOther.
func ReasonOf(code string) Reason {
	switch code {
	case "GEN002", "AUT001", "GEN003":
		return ReasonKeyRejected
	case "SHT009":
		return ReasonSheetReadOnly
	case "SHT001", "SHT008":
		return ReasonSheetMissing
	case "CRD006", "GSH002":
		return ReasonGoogleAccess
	case "OSL015":
		return ReasonKeyUnreadable
	case "OSL016":
		return ReasonPrivateEndpoint
	}
	return ReasonOther
}

func rowMissing(err error) bool {
	nf, ok := errors.AsType[*opensheet.NotFoundError](err)
	return ok && nf.RowMissing()
}

// NOTE: an id already in the tab (409 SHT012), or an Idempotency-Key reused with another body (422 SHT018), both mean an earlier create landed.
func rowTaken(err error) bool {
	if ae, ok := errors.AsType[*opensheet.APIError](err); ok {
		return ae.Status == http.StatusConflict && ae.Code == codeDuplicateID
	}
	ve, ok := errors.AsType[*opensheet.ValidationError](err)
	return ok && ve.Code == codeIdempotencyMismatch
}

// NOTE: *SyncRefusedError when the link itself is refused (any 404 but a missing row, 401/403, 424 such as an expired Google credential CRD006, a sheet-wide code), *RowRefusedError when only this row's values are, and a wrapped error worth a retry otherwise (429, 5xx, 409 SHT017, network).
func refusal(sheet string, err error) error {
	if httpclient.IsPrivateAddressError(err) {
		return &SyncRefusedError{Code: "config", Cause: &PrivateEndpointError{Cause: err}}
	}
	code := codeOf(err)
	if nf, ok := errors.AsType[*opensheet.NotFoundError](err); ok && !nf.RowMissing() {
		return &SyncRefusedError{Sheet: sheet, Code: code, Cause: err}
	}
	if linkLevel(code) {
		return &SyncRefusedError{Sheet: sheet, Code: code, Cause: err}
	}
	if opensheet.IsValidationError(err) || opensheet.IsPreconditionFailedError(err) || opensheet.IsPayloadTooLargeError(err) {
		return &RowRefusedError{Sheet: sheet, Code: code, Cause: err}
	}
	ae, ok := errors.AsType[*opensheet.APIError](err)
	if !ok || transient(ae) {
		return fmt.Errorf("opensheetsync: sheet %s: %w", sheet, err)
	}
	if ae.Status == http.StatusUnauthorized || ae.Status == http.StatusForbidden || ae.Status == http.StatusFailedDependency {
		return &SyncRefusedError{Sheet: sheet, Code: code, Cause: err}
	}
	if ae.Status >= http.StatusBadRequest {
		return &RowRefusedError{Sheet: sheet, Code: code, Cause: err}
	}
	return fmt.Errorf("opensheetsync: sheet %s: %w", sheet, err)
}

func transient(ae *opensheet.APIError) bool {
	return ae.Status >= http.StatusInternalServerError || (ae.Status == http.StatusConflict && ae.Code == codeWriteInFlight)
}

// NOTE: these codes name the whole tab (its header row or its contract), which every row shares, so retrying row by row only spends attempts; SHT010, SHT011, SHT021 and SHT023 arrive as 409, SHT014 as 400 and SHT029 as 422.
func linkLevel(code string) bool {
	switch code {
	case codeNoIDColumn, codeAmbiguousIDColumn, codeUnknownColumn, codeDuplicateColumn, codeContractViolation, codeSoftDeleteUnsupported:
		return true
	}
	return false
}

func codeOf(err error) string {
	if nf, ok := errors.AsType[*opensheet.NotFoundError](err); ok {
		return nf.Code
	}
	if ve, ok := errors.AsType[*opensheet.ValidationError](err); ok {
		return ve.Code
	}
	if ae, ok := errors.AsType[*opensheet.APIError](err); ok {
		return ae.Code
	}
	return ""
}

// SECURITY: last_error is readable by any yasaku:read key, so it carries the typed message, the code and opensheet's own message, never a cause's text (hosts, URLs, sealer detail).
func refusalText(err error) string {
	if ku, ok := errors.AsType[*KeyUnreadableError](err); ok {
		ae := ku.ToAppError()
		return ae.Message() + " (" + ae.Code() + ")"
	}
	if pe, ok := errors.AsType[*PrivateEndpointError](err); ok {
		ae := pe.ToAppError()
		return ae.Message() + " (" + ae.Code() + ")"
	}
	if rn, ok := errors.AsType[*ReconnectNeededError](err); ok {
		ae := rn.ToAppError()
		return ae.Message() + " (" + ae.Code() + ")"
	}
	sr, ok := errors.AsType[*SyncRefusedError](err)
	if !ok {
		return "opensheet refused the sync"
	}
	if sr.Sheet == "" {
		return "opensheet refused the sync (" + sr.Code + ")"
	}
	return typedText(sr.ToAppError().Message(), sr.Code, sr.Cause)
}

// SECURITY: a row's last_error follows refusalText's rule, so a transport cause never puts a host or URL in it.
func failureText(err error) string {
	if IsSyncRefusedError(err) {
		return refusalText(err)
	}
	if rr, ok := errors.AsType[*RowRefusedError](err); ok {
		return typedText(rr.ToAppError().Message(), rr.Code, rr.Cause)
	}
	const retried = "; the row is retried"
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		reason := context.Canceled.Error()
		if errors.Is(err, context.DeadlineExceeded) {
			reason = context.DeadlineExceeded.Error()
		}
		return "the sync ran out of time (" + reason + ")" + retried
	}
	if opensheet.IsRateLimitedError(err) {
		return "opensheet rate limited the sync" + retried
	}
	if ae, ok := errors.AsType[*opensheet.APIError](err); ok {
		return "opensheet failed (" + ae.Code + ")" + retried
	}
	if _, ok := errors.AsType[*url.Error](err); ok {
		return "opensheet could not be reached" + retried
	}
	return "the sync failed" + retried
}

func typedText(msg, code string, cause error) string {
	if detail := opensheetMessage(cause); detail != "" && detail != code {
		return msg + ": " + strings.TrimSpace(code+" "+detail)
	}
	if code == "" {
		return msg
	}
	return msg + " (" + code + ")"
}

func opensheetMessage(err error) string {
	if ae, ok := errors.AsType[*opensheet.APIError](err); ok {
		return ae.Message
	}
	if ve, ok := errors.AsType[*opensheet.ValidationError](err); ok {
		return ve.Message
	}
	return ""
}
