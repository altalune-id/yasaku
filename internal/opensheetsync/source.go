package opensheetsync

import (
	"context"

	"github.com/google/uuid"
)

// Source reads a row's current state as the sheet shows it; found is false once the row is gone.
type Source interface {
	Transaction(ctx context.Context, id uuid.UUID) (TransactionFacts, bool, error)
	Wallet(ctx context.Context, id uuid.UUID) (WalletFacts, bool, error)
	Category(ctx context.Context, id uuid.UUID) (CategoryFacts, bool, error)
}
