package transaction

import (
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"

	apperrorv1 "altalune.id/yasaku/gen/go/apperror/v1"
	"altalune.id/yasaku/internal/apperror"
)

// ErrNoUnitOfWork reports a call that must run inside a transaction but found none open.
var ErrNoUnitOfWork = errors.New("transaction: no unit of work is open")

// NotFoundError reports that no transaction matches ID.
type NotFoundError struct{ ID string }

func (e *NotFoundError) Error() string { return fmt.Sprintf("transaction: %q: not found", e.ID) }

// ToAppError converts the typed error into the wire envelope.
func (e *NotFoundError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeTransactionNotFound,
		"Transaction not found",
		codes.NotFound,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeTransactionNotFound,
			Meta: map[string]string{"transaction_id": e.ID},
		},
	)
}

// IsNotFoundError reports whether err's chain contains a *NotFoundError.
func IsNotFoundError(err error) bool {
	_, ok := errors.AsType[*NotFoundError](err)
	return ok
}

// InvalidAmountError reports an amount that is not a positive quantity of a known currency.
type InvalidAmountError struct{ Reason string }

func (e *InvalidAmountError) Error() string { return "transaction: amount: " + e.Reason }

// ToAppError converts the typed error into the wire envelope.
func (e *InvalidAmountError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeTransactionInvalidAmount,
		"Invalid amount: "+e.Reason,
		codes.InvalidArgument,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeTransactionInvalidAmount,
			Meta: map[string]string{"reason": e.Reason},
		},
	)
}

// IsInvalidAmountError reports whether err's chain contains an *InvalidAmountError.
func IsInvalidAmountError(err error) bool {
	_, ok := errors.AsType[*InvalidAmountError](err)
	return ok
}

// CurrencyMismatchError reports an amount whose currency differs from the wallet's.
type CurrencyMismatchError struct{ WalletID, WalletCurrency, AmountCurrency string }

func (e *CurrencyMismatchError) Error() string {
	return fmt.Sprintf("transaction: currency: wallet %s holds %s, amount is %s",
		e.WalletID, e.WalletCurrency, e.AmountCurrency)
}

// ToAppError converts the typed error into the wire envelope.
func (e *CurrencyMismatchError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeTransactionCurrencyMismatch,
		fmt.Sprintf("This wallet holds %s, not %s", e.WalletCurrency, e.AmountCurrency),
		codes.InvalidArgument,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeTransactionCurrencyMismatch,
			Meta: map[string]string{
				"wallet_id":       e.WalletID,
				"wallet_currency": e.WalletCurrency,
				"amount_currency": e.AmountCurrency,
			},
		},
	)
}

// IsCurrencyMismatchError reports whether err's chain contains a *CurrencyMismatchError.
func IsCurrencyMismatchError(err error) bool {
	_, ok := errors.AsType[*CurrencyMismatchError](err)
	return ok
}

// InvalidKindError reports a kind that is unknown or that disagrees with the fields it carries.
type InvalidKindError struct{ Kind, Reason string }

func (e *InvalidKindError) Error() string {
	return fmt.Sprintf("transaction: kind %q: %s", e.Kind, e.Reason)
}

// ToAppError converts the typed error into the wire envelope.
func (e *InvalidKindError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeTransactionInvalidKind,
		"Invalid transaction kind: "+e.Reason,
		codes.InvalidArgument,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeTransactionInvalidKind,
			Meta: map[string]string{"kind": e.Kind, "reason": e.Reason},
		},
	)
}

// IsInvalidKindError reports whether err's chain contains an *InvalidKindError.
func IsInvalidKindError(err error) bool {
	_, ok := errors.AsType[*InvalidKindError](err)
	return ok
}

// SameWalletError reports a transfer whose source and destination are the same wallet.
type SameWalletError struct{ WalletID string }

func (e *SameWalletError) Error() string {
	return "transaction: transfer: source and destination wallet are both " + e.WalletID
}

// ToAppError converts the typed error into the wire envelope.
func (e *SameWalletError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeTransactionSameWallet,
		"A transfer needs two different wallets",
		codes.InvalidArgument,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeTransactionSameWallet,
			Meta: map[string]string{"wallet_id": e.WalletID},
		},
	)
}

// IsSameWalletError reports whether err's chain contains a *SameWalletError.
func IsSameWalletError(err error) bool {
	_, ok := errors.AsType[*SameWalletError](err)
	return ok
}

// CategoryKindMismatchError reports a category whose kind disagrees with the transaction's.
type CategoryKindMismatchError struct{ CategoryID, CategoryKind, TransactionKind string }

func (e *CategoryKindMismatchError) Error() string {
	return fmt.Sprintf("transaction: category %s is %s, transaction is %s",
		e.CategoryID, e.CategoryKind, e.TransactionKind)
}

// ToAppError converts the typed error into the wire envelope.
func (e *CategoryKindMismatchError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeTransactionCategoryKindMismatch,
		fmt.Sprintf("That is an %s category, not an %s one", e.CategoryKind, e.TransactionKind),
		codes.InvalidArgument,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeTransactionCategoryKindMismatch,
			Meta: map[string]string{
				"category_id":      e.CategoryID,
				"category_kind":    e.CategoryKind,
				"transaction_kind": e.TransactionKind,
			},
		},
	)
}

// IsCategoryKindMismatchError reports whether err's chain contains a *CategoryKindMismatchError.
func IsCategoryKindMismatchError(err error) bool {
	_, ok := errors.AsType[*CategoryKindMismatchError](err)
	return ok
}

// WalletArchivedError reports a write against an archived wallet.
type WalletArchivedError struct{ WalletID string }

func (e *WalletArchivedError) Error() string {
	return "transaction: wallet " + e.WalletID + ": archived"
}

// ToAppError converts the typed error into the wire envelope.
func (e *WalletArchivedError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeTransactionWalletArchived,
		"That wallet is archived",
		codes.FailedPrecondition,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeTransactionWalletArchived,
			Meta: map[string]string{"wallet_id": e.WalletID},
		},
	)
}

// IsWalletArchivedError reports whether err's chain contains a *WalletArchivedError.
func IsWalletArchivedError(err error) bool {
	_, ok := errors.AsType[*WalletArchivedError](err)
	return ok
}

// PeriodLockedError reports a write against a closed budget period.
type PeriodLockedError struct{ PeriodID string }

func (e *PeriodLockedError) Error() string {
	return "transaction: period " + e.PeriodID + ": locked"
}

// ToAppError converts the typed error into the wire envelope.
func (e *PeriodLockedError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeTransactionPeriodLocked,
		"That budget period is closed",
		codes.FailedPrecondition,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeTransactionPeriodLocked,
			Meta: map[string]string{"period_id": e.PeriodID},
		},
	)
}

// IsPeriodLockedError reports whether err's chain contains a *PeriodLockedError.
func IsPeriodLockedError(err error) bool {
	_, ok := errors.AsType[*PeriodLockedError](err)
	return ok
}

// PeriodNotAdjacentError reports a period that neither contains the transaction nor neighbours the one that does.
type PeriodNotAdjacentError struct{ PeriodID string }

func (e *PeriodNotAdjacentError) Error() string {
	return "transaction: period " + e.PeriodID + ": not the containing period or one of its neighbours"
}

// ToAppError converts the typed error into the wire envelope.
func (e *PeriodNotAdjacentError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeTransactionPeriodNotAdjacent,
		"A transaction may only be moved to the period before or after the one it falls in",
		codes.InvalidArgument,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeTransactionPeriodNotAdjacent,
			Meta: map[string]string{"period_id": e.PeriodID},
		},
	)
}

// IsPeriodNotAdjacentError reports whether err's chain contains a *PeriodNotAdjacentError.
func IsPeriodNotAdjacentError(err error) bool {
	_, ok := errors.AsType[*PeriodNotAdjacentError](err)
	return ok
}

// InvalidNoteError reports a note that violates a creation invariant.
type InvalidNoteError struct{ Reason string }

func (e *InvalidNoteError) Error() string { return "transaction: note: " + e.Reason }

// ToAppError converts the typed error into the wire envelope.
func (e *InvalidNoteError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeTransactionInvalidNote,
		"Invalid note: "+e.Reason,
		codes.InvalidArgument,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeTransactionInvalidNote,
			Meta: map[string]string{"reason": e.Reason},
		},
	)
}

// IsInvalidNoteError reports whether err's chain contains an *InvalidNoteError.
func IsInvalidNoteError(err error) bool {
	_, ok := errors.AsType[*InvalidNoteError](err)
	return ok
}
