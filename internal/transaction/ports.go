package transaction

import (
	"context"
	"time"

	"github.com/google/uuid"

	"altalune.id/yasaku/money"
)

// WalletInfo is the slice of a wallet this module needs to validate a write.
type WalletInfo struct {
	ID       uuid.UUID
	Currency money.Currency
	Archived bool
}

// WalletReader resolves a wallet in the caller's scope.
type WalletReader interface {
	Wallet(ctx context.Context, orgID, projectID, id uuid.UUID) (WalletInfo, error)
}

// WalletReaderFunc adapts a function to WalletReader.
type WalletReaderFunc func(ctx context.Context, orgID, projectID, id uuid.UUID) (WalletInfo, error)

// Wallet calls f.
func (f WalletReaderFunc) Wallet(ctx context.Context, orgID, projectID, id uuid.UUID) (WalletInfo, error) {
	return f(ctx, orgID, projectID, id)
}

// CategoryInfo is the slice of a category this module needs to validate a write.
type CategoryInfo struct {
	ID       uuid.UUID
	Kind     string
	Archived bool
}

// CategoryReader resolves a category in the caller's scope.
type CategoryReader interface {
	Category(ctx context.Context, orgID, projectID, id uuid.UUID) (CategoryInfo, error)
}

// CategoryReaderFunc adapts a function to CategoryReader.
type CategoryReaderFunc func(ctx context.Context, orgID, projectID, id uuid.UUID) (CategoryInfo, error)

// Category calls f.
func (f CategoryReaderFunc) Category(ctx context.Context, orgID, projectID, id uuid.UUID) (CategoryInfo, error) {
	return f(ctx, orgID, projectID, id)
}

// PeriodInfo is the slice of a budget period this module needs, plus its neighbours for payday carryover.
type PeriodInfo struct {
	ID     uuid.UUID
	Locked bool
	PrevID *uuid.UUID
	NextID *uuid.UUID
}

// PeriodResolver maps an instant, or an id, to a budget period.
type PeriodResolver interface {
	// Containing reports the period covering at; ok is false when no period does.
	Containing(ctx context.Context, orgID, projectID uuid.UUID, at time.Time) (PeriodInfo, bool, error)
	ByID(ctx context.Context, orgID, projectID, id uuid.UUID) (PeriodInfo, error)
}

// Suggester proposes a category for a note.
type Suggester interface {
	Suggest(ctx context.Context, orgID, projectID uuid.UUID, note string) (uuid.UUID, bool, error)
}

// UnitOfWork runs fn inside one database transaction.
type UnitOfWork func(ctx context.Context, fn func(ctx context.Context) error) error
