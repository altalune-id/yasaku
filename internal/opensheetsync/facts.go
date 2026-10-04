package opensheetsync

import (
	"time"

	"github.com/google/uuid"

	"altalune.id/yasaku/money"
)

// TransactionFacts is a transaction as the sheet shows it, names resolved; uuid.Nil marks an absent reference.
type TransactionFacts struct {
	ID           uuid.UUID
	Kind         string
	Amount       money.Amount
	OccurredAt   time.Time
	Location     *time.Location
	WalletID     uuid.UUID
	WalletName   string
	ToWalletID   uuid.UUID
	ToWalletName string
	CategoryID   uuid.UUID
	CategoryName string
	PeriodName   string
	Note         string
	RecurringID  uuid.UUID
	UpdatedAt    time.Time
}

// WalletFacts is a wallet as the sheet shows it, with its live balance.
type WalletFacts struct {
	ID               uuid.UUID
	Name             string
	Kind             string
	Provider         string
	Balance          money.Amount
	ExcludeFromTotal bool
	Archived         bool
	UpdatedAt        time.Time
}

// CategoryFacts is a category as the sheet shows it.
type CategoryFacts struct {
	ID        uuid.UUID
	Name      string
	Kind      string
	Icon      string
	Color     string
	Archived  bool
	UpdatedAt time.Time
}
