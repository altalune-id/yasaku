package period

import (
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"

	apperrorv1 "altalune.id/yasaku/gen/go/apperror/v1"
	"altalune.id/yasaku/internal/apperror"
)

// NotFoundError reports that no period matches ID in the caller's scope.
type NotFoundError struct{ ID string }

func (e *NotFoundError) Error() string { return fmt.Sprintf("period: %q: not found", e.ID) }

// ToAppError converts the typed error into the wire envelope.
func (e *NotFoundError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodePeriodNotFound,
		"Period not found",
		codes.NotFound,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodePeriodNotFound,
			Meta: map[string]string{"period_id": e.ID},
		},
	)
}

// IsNotFoundError reports whether err's chain contains a *NotFoundError.
func IsNotFoundError(err error) bool {
	_, ok := errors.AsType[*NotFoundError](err)
	return ok
}

// InvalidNameError reports that a period name violates an invariant.
type InvalidNameError struct{ Reason string }

func (e *InvalidNameError) Error() string { return "period: name: " + e.Reason }

// ToAppError converts the typed error into the wire envelope.
func (e *InvalidNameError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodePeriodInvalidName,
		"Invalid period name: "+e.Reason,
		codes.InvalidArgument,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodePeriodInvalidName,
			Meta: map[string]string{"reason": e.Reason},
		},
	)
}

// IsInvalidNameError reports whether err's chain contains an *InvalidNameError.
func IsInvalidNameError(err error) bool {
	_, ok := errors.AsType[*InvalidNameError](err)
	return ok
}

// InvalidRangeError reports that the requested period boundaries are not usable.
type InvalidRangeError struct{ Reason string }

func (e *InvalidRangeError) Error() string { return "period: range: " + e.Reason }

// ToAppError converts the typed error into the wire envelope.
func (e *InvalidRangeError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodePeriodInvalidRange,
		"Invalid period range: "+e.Reason,
		codes.InvalidArgument,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodePeriodInvalidRange,
			Meta: map[string]string{"reason": e.Reason},
		},
	)
}

// IsInvalidRangeError reports whether err's chain contains an *InvalidRangeError.
func IsInvalidRangeError(err error) bool {
	_, ok := errors.AsType[*InvalidRangeError](err)
	return ok
}

// OverlapError reports that the write would leave the project with a second current period.
type OverlapError struct{ Start, End string }

func (e *OverlapError) Error() string {
	return fmt.Sprintf("period: range %s..%s: overlaps an existing period", e.Start, e.End)
}

// ToAppError converts the typed error into the wire envelope.
func (e *OverlapError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodePeriodOverlap,
		"This period overlaps an existing one",
		codes.AlreadyExists,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodePeriodOverlap,
			Meta: map[string]string{"start": e.Start, "end": e.End},
		},
	)
}

// IsOverlapError reports whether err's chain contains an *OverlapError.
func IsOverlapError(err error) bool {
	_, ok := errors.AsType[*OverlapError](err)
	return ok
}

// AlreadyClosedError reports that the period is closed and must be reopened first.
type AlreadyClosedError struct{ ID string }

func (e *AlreadyClosedError) Error() string { return fmt.Sprintf("period: %q: already closed", e.ID) }

// ToAppError converts the typed error into the wire envelope.
func (e *AlreadyClosedError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodePeriodAlreadyClosed,
		"This period is already closed",
		codes.FailedPrecondition,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodePeriodAlreadyClosed,
			Meta: map[string]string{"period_id": e.ID},
		},
	)
}

// IsAlreadyClosedError reports whether err's chain contains an *AlreadyClosedError.
func IsAlreadyClosedError(err error) bool {
	_, ok := errors.AsType[*AlreadyClosedError](err)
	return ok
}

// NotClosedError reports that the operation needs a closed period.
type NotClosedError struct{ ID string }

func (e *NotClosedError) Error() string { return fmt.Sprintf("period: %q: not closed", e.ID) }

// ToAppError converts the typed error into the wire envelope.
func (e *NotClosedError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodePeriodNotClosed,
		"This period is not closed",
		codes.FailedPrecondition,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodePeriodNotClosed,
			Meta: map[string]string{"period_id": e.ID},
		},
	)
}

// IsNotClosedError reports whether err's chain contains a *NotClosedError.
func IsNotClosedError(err error) bool {
	_, ok := errors.AsType[*NotClosedError](err)
	return ok
}

// NotLatestClosedError reports that only the most recently closed period may be reopened.
type NotLatestClosedError struct{ ID string }

func (e *NotLatestClosedError) Error() string {
	return fmt.Sprintf("period: %q: not the latest closed period", e.ID)
}

// ToAppError converts the typed error into the wire envelope.
func (e *NotLatestClosedError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodePeriodNotLatestClosed,
		"Only the most recently closed period can be reopened",
		codes.FailedPrecondition,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodePeriodNotLatestClosed,
			Meta: map[string]string{"period_id": e.ID},
		},
	)
}

// IsNotLatestClosedError reports whether err's chain contains a *NotLatestClosedError.
func IsNotLatestClosedError(err error) bool {
	_, ok := errors.AsType[*NotLatestClosedError](err)
	return ok
}

func isDomainError(err error) bool {
	return IsNotFoundError(err) || IsInvalidNameError(err) || IsInvalidRangeError(err) ||
		IsOverlapError(err) || IsAlreadyClosedError(err) || IsNotClosedError(err) ||
		IsNotLatestClosedError(err)
}
