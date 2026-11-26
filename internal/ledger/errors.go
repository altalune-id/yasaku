package ledger

import (
	"errors"
	"fmt"
	"strconv"

	"google.golang.org/grpc/codes"

	apperrorv1 "altalune.id/yasaku/gen/go/apperror/v1"
	"altalune.id/yasaku/internal/apperror"
)

// NotFoundError reports that a project has no stored ledger settings row.
type NotFoundError struct{ ProjectID string }

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("ledger: settings: not found: project_id=%s", e.ProjectID)
}

// ToAppError converts the typed error into the wire envelope.
func (e *NotFoundError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeNotFound,
		"Ledger settings not found",
		codes.NotFound,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeNotFound,
			Meta: map[string]string{"project_id": e.ProjectID},
		},
	)
}

// IsNotFoundError reports whether err's chain contains a *NotFoundError.
func IsNotFoundError(err error) bool {
	_, ok := errors.AsType[*NotFoundError](err)
	return ok
}

// InvalidTimezoneError reports a timezone that is not a known IANA zone.
type InvalidTimezoneError struct{ Value string }

func (e *InvalidTimezoneError) Error() string {
	return fmt.Sprintf("ledger: timezone: not a known IANA zone: %q", e.Value)
}

// ToAppError converts the typed error into the wire envelope.
func (e *InvalidTimezoneError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeLedgerInvalidTimezone,
		fmt.Sprintf("%q is not a known timezone", e.Value),
		codes.InvalidArgument,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeLedgerInvalidTimezone,
			Meta: map[string]string{"timezone": e.Value},
		},
	)
}

// IsInvalidTimezoneError reports whether err's chain contains an *InvalidTimezoneError.
func IsInvalidTimezoneError(err error) bool {
	_, ok := errors.AsType[*InvalidTimezoneError](err)
	return ok
}

// InvalidStartDayError reports a period start day outside 1..28.
type InvalidStartDayError struct{ Value int }

func (e *InvalidStartDayError) Error() string {
	return fmt.Sprintf("ledger: period start day: outside %d..%d: %d", MinPeriodStartDay, MaxPeriodStartDay, e.Value)
}

// ToAppError converts the typed error into the wire envelope.
func (e *InvalidStartDayError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeLedgerInvalidStartDay,
		fmt.Sprintf("Period start day must be between %d and %d", MinPeriodStartDay, MaxPeriodStartDay),
		codes.InvalidArgument,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeLedgerInvalidStartDay,
			Meta: map[string]string{"period_start_day": strconv.Itoa(e.Value)},
		},
	)
}

// IsInvalidStartDayError reports whether err's chain contains an *InvalidStartDayError.
func IsInvalidStartDayError(err error) bool {
	_, ok := errors.AsType[*InvalidStartDayError](err)
	return ok
}

// UnknownCurrencyError reports a currency code outside the supported table.
type UnknownCurrencyError struct{ Code string }

func (e *UnknownCurrencyError) Error() string {
	return fmt.Sprintf("ledger: currency: unsupported: %q", e.Code)
}

// ToAppError converts the typed error into the wire envelope.
func (e *UnknownCurrencyError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeLedgerUnknownCurrency,
		fmt.Sprintf("%q is not a supported currency", e.Code),
		codes.InvalidArgument,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeLedgerUnknownCurrency,
			Meta: map[string]string{"currency": e.Code},
		},
	)
}

// IsUnknownCurrencyError reports whether err's chain contains an *UnknownCurrencyError.
func IsUnknownCurrencyError(err error) bool {
	_, ok := errors.AsType[*UnknownCurrencyError](err)
	return ok
}

// WipeConfirmMismatchError refuses a wipe whose confirmation is not the project's name.
type WipeConfirmMismatchError struct{}

func (e *WipeConfirmMismatchError) Error() string {
	return "ledger: wipe: confirmation does not match the project name"
}

// ToAppError converts the typed error into the wire envelope.
func (e *WipeConfirmMismatchError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeDataWipeConfirmMismatch,
		"Type the project name exactly to wipe its data",
		codes.InvalidArgument,
		&apperrorv1.ErrorDetail{Code: apperror.CodeDataWipeConfirmMismatch},
	)
}

// IsWipeConfirmMismatchError reports whether err's chain contains a *WipeConfirmMismatchError.
func IsWipeConfirmMismatchError(err error) bool {
	_, ok := errors.AsType[*WipeConfirmMismatchError](err)
	return ok
}

// WipeOwnerOnlyError refuses a wipe by anyone but a built-in owner of the project's org.
type WipeOwnerOnlyError struct{}

func (e *WipeOwnerOnlyError) Error() string {
	return "ledger: wipe: only an owner can wipe a project"
}

// ToAppError converts the typed error into the wire envelope.
func (e *WipeOwnerOnlyError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodePermissionDenied,
		"Only an owner can wipe this project's data",
		codes.PermissionDenied,
		&apperrorv1.ErrorDetail{Code: apperror.CodePermissionDenied, Meta: map[string]string{"role": "owner"}},
	)
}

// IsWipeOwnerOnlyError reports whether err's chain contains a *WipeOwnerOnlyError.
func IsWipeOwnerOnlyError(err error) bool {
	_, ok := errors.AsType[*WipeOwnerOnlyError](err)
	return ok
}

// WipeSetupIncompleteError reports a wipe that committed but whose reseed of default categories or current period failed.
type WipeSetupIncompleteError struct{ Cause error }

func (e *WipeSetupIncompleteError) Error() string {
	return "ledger: wipe: data wiped, setup failed: " + e.Cause.Error()
}

func (e *WipeSetupIncompleteError) Unwrap() error { return e.Cause }

// IsWipeSetupIncompleteError reports whether err's chain contains a *WipeSetupIncompleteError.
func IsWipeSetupIncompleteError(err error) bool {
	_, ok := errors.AsType[*WipeSetupIncompleteError](err)
	return ok
}
