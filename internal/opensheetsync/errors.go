package opensheetsync

import (
	"errors"
	"strings"

	"google.golang.org/grpc/codes"

	apperrorv1 "altalune.id/yasaku/gen/go/apperror/v1"
	"altalune.id/yasaku/internal/apperror"
)

func appErr(code, msg string, c codes.Code, meta map[string]string) *apperror.AppError {
	return apperror.New(code, msg, c, &apperrorv1.ErrorDetail{Code: code, Meta: meta})
}

func causeText(err error) string {
	if err == nil {
		return "no cause"
	}
	return err.Error()
}

// LinkNotFoundError reports that the project has no opensheet link.
type LinkNotFoundError struct{ ProjectID string }

func (e *LinkNotFoundError) Error() string {
	return "opensheetsync: link: not found: project=" + e.ProjectID
}

// ToAppError converts the typed error into the wire envelope.
func (e *LinkNotFoundError) ToAppError() *apperror.AppError {
	return appErr(apperror.CodeOpensheetLinkNotFound, "This project has no Opensheet link", codes.NotFound, map[string]string{"project_id": e.ProjectID})
}

// IsLinkNotFoundError reports whether err's tree contains a *LinkNotFoundError.
func IsLinkNotFoundError(err error) bool {
	_, ok := errors.AsType[*LinkNotFoundError](err)
	return ok
}

// InvalidSettingError reports a form field that breaks its rule.
type InvalidSettingError struct{ Field, Reason string }

func (e *InvalidSettingError) Error() string {
	return "opensheetsync: settings: invalid " + e.Field + ": " + e.Reason
}

// ToAppError converts the typed error into the wire envelope.
func (e *InvalidSettingError) ToAppError() *apperror.AppError {
	return appErr(apperror.CodeOpensheetInvalidSetting, "Invalid "+e.Field+": "+e.Reason, codes.InvalidArgument,
		map[string]string{"field": e.Field, "reason": e.Reason})
}

// IsInvalidSettingError reports whether err's tree contains an *InvalidSettingError.
func IsInvalidSettingError(err error) bool {
	_, ok := errors.AsType[*InvalidSettingError](err)
	return ok
}

// APIKeyRequiredError reports a first Save or Test with no API key entered.
type APIKeyRequiredError struct{}

func (e *APIKeyRequiredError) Error() string { return "opensheetsync: settings: api key required" }

// ToAppError converts the typed error into the wire envelope.
func (e *APIKeyRequiredError) ToAppError() *apperror.AppError {
	return appErr(apperror.CodeOpensheetAPIKeyRequired, "Enter the opensheet API key", codes.InvalidArgument, nil)
}

// IsAPIKeyRequiredError reports whether err's tree contains an *APIKeyRequiredError.
func IsAPIKeyRequiredError(err error) bool {
	_, ok := errors.AsType[*APIKeyRequiredError](err)
	return ok
}

// SheetUnreachableError reports a sheet opensheet will not serve to this key: the 404 mask, a 401 or a 403.
type SheetUnreachableError struct{ Sheet string }

func (e *SheetUnreachableError) Error() string {
	return "opensheetsync: test: sheet unreachable: " + e.Sheet
}

// ToAppError converts the typed error into the wire envelope.
func (e *SheetUnreachableError) ToAppError() *apperror.AppError {
	return appErr(apperror.CodeOpensheetSheetUnreachable, "opensheet does not serve sheet "+e.Sheet+" to this key",
		codes.FailedPrecondition, map[string]string{"sheet": e.Sheet})
}

// IsSheetUnreachableError reports whether err's tree contains a *SheetUnreachableError.
func IsSheetUnreachableError(err error) bool {
	_, ok := errors.AsType[*SheetUnreachableError](err)
	return ok
}

const staleHeaderHint = "If you just fixed the header, opensheet may still have the old header; open the tab's rows in opensheet (or wait a few minutes) and run Test again"

// ShapeMismatchError reports contract columns missing from a sheet's header row.
type ShapeMismatchError struct {
	Sheet   string
	Missing []string
}

func (e *ShapeMismatchError) Error() string {
	return "opensheetsync: test: sheet " + e.Sheet + " misses columns: " + strings.Join(e.Missing, ",")
}

// ToAppError converts the typed error into the wire envelope.
func (e *ShapeMismatchError) ToAppError() *apperror.AppError {
	return appErr(apperror.CodeOpensheetShapeMismatch, "Sheet "+e.Sheet+" is missing columns: "+strings.Join(e.Missing, ", ")+". "+staleHeaderHint,
		codes.FailedPrecondition, map[string]string{"sheet": e.Sheet, "missing": strings.Join(e.Missing, ",")})
}

// IsShapeMismatchError reports whether err's tree contains a *ShapeMismatchError.
func IsShapeMismatchError(err error) bool {
	_, ok := errors.AsType[*ShapeMismatchError](err)
	return ok
}

// NoIDColumnError reports a sheet published without id as its id column.
type NoIDColumnError struct{ Sheet string }

func (e *NoIDColumnError) Error() string {
	return "opensheetsync: test: sheet " + e.Sheet + " has no id column"
}

// ToAppError converts the typed error into the wire envelope.
func (e *NoIDColumnError) ToAppError() *apperror.AppError {
	return appErr(apperror.CodeOpensheetNoIDColumn, "Set id as the id column of sheet "+e.Sheet, codes.FailedPrecondition, map[string]string{"sheet": e.Sheet})
}

// IsNoIDColumnError reports whether err's tree contains a *NoIDColumnError.
func IsNoIDColumnError(err error) bool {
	_, ok := errors.AsType[*NoIDColumnError](err)
	return ok
}

// SheetNotWritableError reports a sheet not marked writable in opensheet.
type SheetNotWritableError struct{ Sheet string }

func (e *SheetNotWritableError) Error() string {
	return "opensheetsync: test: sheet " + e.Sheet + " is not writable"
}

// ToAppError converts the typed error into the wire envelope.
func (e *SheetNotWritableError) ToAppError() *apperror.AppError {
	return appErr(apperror.CodeOpensheetSheetNotWritable, "Mark sheet "+e.Sheet+" writable in opensheet", codes.FailedPrecondition, map[string]string{"sheet": e.Sheet})
}

// IsSheetNotWritableError reports whether err's tree contains a *SheetNotWritableError.
func IsSheetNotWritableError(err error) bool {
	_, ok := errors.AsType[*SheetNotWritableError](err)
	return ok
}

// ContractUnsatisfiedError reports a sheet opensheet itself cannot address by row, with opensheet's reason.
type ContractUnsatisfiedError struct{ Sheet, Reason string }

func (e *ContractUnsatisfiedError) Error() string {
	return "opensheetsync: test: sheet " + e.Sheet + " fails the table contract: " + e.Reason
}

// ToAppError converts the typed error into the wire envelope.
func (e *ContractUnsatisfiedError) ToAppError() *apperror.AppError {
	return appErr(apperror.CodeOpensheetContractUnsatisfied, "Sheet "+e.Sheet+": "+e.Reason, codes.FailedPrecondition,
		map[string]string{"sheet": e.Sheet, "reason": e.Reason})
}

// IsContractUnsatisfiedError reports whether err's tree contains a *ContractUnsatisfiedError.
func IsContractUnsatisfiedError(err error) bool {
	_, ok := errors.AsType[*ContractUnsatisfiedError](err)
	return ok
}

// UnavailableError reports opensheet failing to answer for a sheet: a timeout, a rate limit or a 5xx.
type UnavailableError struct{ Sheet, Cause string }

func (e *UnavailableError) Error() string {
	return "opensheetsync: opensheet unavailable for " + e.Sheet + ": " + e.Cause
}

// ToAppError converts the typed error into the wire envelope.
func (e *UnavailableError) ToAppError() *apperror.AppError {
	return appErr(apperror.CodeOpensheetUnavailable, "opensheet did not answer; try again", codes.Unavailable, map[string]string{"sheet": e.Sheet})
}

// IsUnavailableError reports whether err's tree contains an *UnavailableError.
func IsUnavailableError(err error) bool {
	_, ok := errors.AsType[*UnavailableError](err)
	return ok
}

// NotVerifiedError reports turning on a link whose settings have not passed a Test since it was last turned off by failures.
type NotVerifiedError struct{}

func (e *NotVerifiedError) Error() string { return "opensheetsync: enable: link not verified" }

// ToAppError converts the typed error into the wire envelope.
func (e *NotVerifiedError) ToAppError() *apperror.AppError {
	return appErr(apperror.CodeOpensheetNotVerified, "Run Test and Save before turning the mirror on", codes.FailedPrecondition, nil)
}

// IsNotVerifiedError reports whether err's tree contains a *NotVerifiedError.
func IsNotVerifiedError(err error) bool {
	_, ok := errors.AsType[*NotVerifiedError](err)
	return ok
}

// LinkDisabledError reports a sync request on a link that is off.
type LinkDisabledError struct{}

func (e *LinkDisabledError) Error() string { return "opensheetsync: sync: link disabled" }

// ToAppError converts the typed error into the wire envelope.
func (e *LinkDisabledError) ToAppError() *apperror.AppError {
	return appErr(apperror.CodeOpensheetLinkDisabled, "Turn the mirror on first", codes.FailedPrecondition, nil)
}

// IsLinkDisabledError reports whether err's tree contains a *LinkDisabledError.
func IsLinkDisabledError(err error) bool {
	_, ok := errors.AsType[*LinkDisabledError](err)
	return ok
}

// SyncRefusedError reports opensheet refusing the link itself (the 404 mask, 401/403, not writable, no deleted_at, an unreadable key or a bad config), which no retry fixes and which counts toward auto-disable.
type SyncRefusedError struct {
	Sheet string
	Code  string
	Cause error
}

func (e *SyncRefusedError) Error() string {
	return "opensheetsync: sync: sheet " + e.Sheet + " refused (" + e.Code + "): " + causeText(e.Cause)
}

func (e *SyncRefusedError) Unwrap() error { return e.Cause }

// ToAppError converts the typed error into the wire envelope.
func (e *SyncRefusedError) ToAppError() *apperror.AppError {
	return appErr(apperror.CodeOpensheetSyncRefused, "opensheet refused a write to sheet "+e.Sheet, codes.FailedPrecondition,
		map[string]string{"sheet": e.Sheet, "opensheet_code": e.Code})
}

// IsSyncRefusedError reports whether err's tree contains a *SyncRefusedError.
func IsSyncRefusedError(err error) bool {
	_, ok := errors.AsType[*SyncRefusedError](err)
	return ok
}

// ScopeMismatchError reports a sync job whose payload names another project than the tenant it runs as; an empty Got means it runs with no tenant.
type ScopeMismatchError struct{ Want, Got string }

func (e *ScopeMismatchError) Error() string {
	if e.Got == "" {
		return "opensheetsync: sync: payload project " + e.Want + " runs with no tenant"
	}
	return "opensheetsync: sync: payload project " + e.Want + " runs as project " + e.Got
}

// ToAppError converts the typed error into the wire envelope.
func (e *ScopeMismatchError) ToAppError() *apperror.AppError {
	return appErr(apperror.CodeOpensheetScopeMismatch, "Sync job scope mismatch", codes.FailedPrecondition, nil)
}

// IsScopeMismatchError reports whether err's tree contains a *ScopeMismatchError.
func IsScopeMismatchError(err error) bool {
	_, ok := errors.AsType[*ScopeMismatchError](err)
	return ok
}

// RowRefusedError reports opensheet refusing one row's values; the row records it and is retried with backoff, and the link is not penalised.
type RowRefusedError struct {
	Sheet string
	Code  string
	Cause error
}

func (e *RowRefusedError) Error() string {
	return "opensheetsync: sync: sheet " + e.Sheet + " refused a row (" + e.Code + "): " + causeText(e.Cause)
}

func (e *RowRefusedError) Unwrap() error { return e.Cause }

// ToAppError converts the typed error into the wire envelope.
func (e *RowRefusedError) ToAppError() *apperror.AppError {
	return appErr(apperror.CodeOpensheetRowRefused, "opensheet refused a row of sheet "+e.Sheet, codes.FailedPrecondition,
		map[string]string{"sheet": e.Sheet, "opensheet_code": e.Code})
}

// IsRowRefusedError reports whether err's tree contains a *RowRefusedError.
func IsRowRefusedError(err error) bool {
	_, ok := errors.AsType[*RowRefusedError](err)
	return ok
}

// KeyUnreadableError reports a saved API key the deployment's encryption key can no longer open, so the person must enter it again.
type KeyUnreadableError struct{ Cause error }

func (e *KeyUnreadableError) Error() string {
	return "opensheetsync: api key unreadable: " + causeText(e.Cause)
}

func (e *KeyUnreadableError) Unwrap() error { return e.Cause }

// ToAppError converts the typed error into the wire envelope.
func (e *KeyUnreadableError) ToAppError() *apperror.AppError {
	return appErr(apperror.CodeOpensheetKeyUnreadable, "The saved opensheet API key can no longer be read; enter it again and save", codes.FailedPrecondition, nil)
}

// IsKeyUnreadableError reports whether err's tree contains a *KeyUnreadableError.
func IsKeyUnreadableError(err error) bool {
	_, ok := errors.AsType[*KeyUnreadableError](err)
	return ok
}

const privateEndpointMessage = "The opensheet base URL resolves to a private address; set YASAKU_OPENSHEET_ALLOW_PRIVATE_HOSTS=true when opensheet runs on a private network"

// PrivateEndpointError reports opensheet.baseURL resolving to a private address while opensheet.allowPrivateHosts is off, a deployment fault no retry fixes.
type PrivateEndpointError struct{ Cause error }

func (e *PrivateEndpointError) Error() string {
	return "opensheetsync: opensheet base URL resolves to a private address: " + causeText(e.Cause)
}

func (e *PrivateEndpointError) Unwrap() error { return e.Cause }

// ToAppError converts the typed error into the wire envelope.
func (e *PrivateEndpointError) ToAppError() *apperror.AppError {
	return appErr(apperror.CodeOpensheetPrivateEndpoint, privateEndpointMessage, codes.FailedPrecondition, nil)
}

// IsPrivateEndpointError reports whether err's tree contains a *PrivateEndpointError.
func IsPrivateEndpointError(err error) bool {
	_, ok := errors.AsType[*PrivateEndpointError](err)
	return ok
}
