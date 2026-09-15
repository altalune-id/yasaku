package wallet

import (
	"errors"
	"strings"

	"google.golang.org/grpc/codes"

	apperrorv1 "altalune.id/yasaku/gen/go/apperror/v1"
	"altalune.id/yasaku/internal/apperror"
)

// NotFoundError reports that no wallet in the caller's scope matches ID.
type NotFoundError struct{ ID string }

func (e *NotFoundError) Error() string {
	return "wallet: lookup: not found: id=" + e.ID
}

// ToAppError converts the typed error into the wire envelope.
func (e *NotFoundError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeWalletNotFound,
		"Wallet not found",
		codes.NotFound,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeWalletNotFound,
			Meta: map[string]string{"wallet_id": e.ID},
		},
	)
}

// IsNotFoundError reports whether err's tree contains a *NotFoundError.
func IsNotFoundError(err error) bool {
	_, ok := errors.AsType[*NotFoundError](err)
	return ok
}

// InvalidNameError reports that a wallet name violates an invariant.
type InvalidNameError struct{ Reason string }

func (e *InvalidNameError) Error() string {
	return "wallet: name: invalid: " + e.Reason
}

// ToAppError converts the typed error into the wire envelope.
func (e *InvalidNameError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeWalletInvalidName,
		"Invalid wallet name: "+e.Reason,
		codes.InvalidArgument,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeWalletInvalidName,
			Meta: map[string]string{"reason": e.Reason},
		},
	)
}

// IsInvalidNameError reports whether err's tree contains an *InvalidNameError.
func IsInvalidNameError(err error) bool {
	_, ok := errors.AsType[*InvalidNameError](err)
	return ok
}

// AlreadyExistsError reports that an active wallet in this project already holds the name.
type AlreadyExistsError struct{ Name string }

func (e *AlreadyExistsError) Error() string {
	return "wallet: save: name already taken in this project: name=" + e.Name
}

// ToAppError converts the typed error into the wire envelope.
func (e *AlreadyExistsError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeWalletAlreadyExists,
		"A wallet with this name already exists",
		codes.AlreadyExists,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeWalletAlreadyExists,
			Meta: map[string]string{"name": e.Name},
		},
	)
}

// IsAlreadyExistsError reports whether err's tree contains an *AlreadyExistsError.
func IsAlreadyExistsError(err error) bool {
	_, ok := errors.AsType[*AlreadyExistsError](err)
	return ok
}

// InvalidKindError reports a wallet kind outside the supported set.
type InvalidKindError struct{ Value string }

func (e *InvalidKindError) Error() string {
	return "wallet: kind: unsupported: " + e.Value
}

// ToAppError converts the typed error into the wire envelope.
func (e *InvalidKindError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeWalletInvalidKind,
		"Unsupported wallet kind: "+e.Value,
		codes.InvalidArgument,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeWalletInvalidKind,
			Meta: map[string]string{"kind": e.Value},
		},
	)
}

// IsInvalidKindError reports whether err's tree contains an *InvalidKindError.
func IsInvalidKindError(err error) bool {
	_, ok := errors.AsType[*InvalidKindError](err)
	return ok
}

// InUseError reports that the wallet is still referenced by transactions.
type InUseError struct{ ID string }

func (e *InUseError) Error() string {
	return "wallet: delete: still referenced by transactions: id=" + e.ID
}

// ToAppError converts the typed error into the wire envelope.
func (e *InUseError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeWalletInUse,
		"This wallet still has transactions",
		codes.FailedPrecondition,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeWalletInUse,
			Meta: map[string]string{"wallet_id": e.ID},
		},
	)
}

// IsInUseError reports whether err's tree contains an *InUseError.
func IsInUseError(err error) bool {
	_, ok := errors.AsType[*InUseError](err)
	return ok
}

// AmbiguousNameError reports that a name query matched more than one wallet.
type AmbiguousNameError struct {
	Query      string
	Candidates []string
}

func (e *AmbiguousNameError) Error() string {
	return "wallet: resolve: ambiguous name: query=" + e.Query + " candidates=" + strings.Join(e.Candidates, ", ")
}

// ToAppError converts the typed error into the wire envelope.
func (e *AmbiguousNameError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeWalletAmbiguousName,
		"Several wallets match "+e.Query,
		codes.InvalidArgument,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeWalletAmbiguousName,
			Meta: map[string]string{
				"query":      e.Query,
				"candidates": strings.Join(e.Candidates, ", "),
			},
		},
	)
}

// IsAmbiguousNameError reports whether err's tree contains an *AmbiguousNameError.
func IsAmbiguousNameError(err error) bool {
	_, ok := errors.AsType[*AmbiguousNameError](err)
	return ok
}

// ArchivedError reports an edit attempted on an archived wallet.
type ArchivedError struct{ ID string }

func (e *ArchivedError) Error() string {
	return "wallet: edit: wallet is archived: id=" + e.ID
}

// ToAppError converts the typed error into the wire envelope.
func (e *ArchivedError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeWalletArchived,
		"This wallet is archived; unarchive it first",
		codes.FailedPrecondition,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeWalletArchived,
			Meta: map[string]string{"wallet_id": e.ID},
		},
	)
}

// IsArchivedError reports whether err's tree contains an *ArchivedError.
func IsArchivedError(err error) bool {
	_, ok := errors.AsType[*ArchivedError](err)
	return ok
}
