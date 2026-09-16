package report

import (
	"context"
	"time"

	"github.com/google/uuid"

	"altalune.id/yasaku/money"
)

// Reader is the driven port: every method is read-only and tenant-scoped by its arguments.
type Reader interface {
	// Period resolves the period's name and date range so the Service can derive the period's local start instant.
	Period(ctx context.Context, orgID, projectID, periodID uuid.UUID) (PeriodRef, error)
	// Summary totals one period in one currency; startUTC is the period's start at local midnight and bounds unassigned carry-in.
	Summary(ctx context.Context, orgID, projectID, periodID uuid.UUID, currency money.Currency, startUTC time.Time) (PeriodSummary, error)
	SpendByCategory(ctx context.Context, orgID, projectID, periodID uuid.UUID, currency money.Currency) ([]CategorySlice, error)
	IncomeByCategory(ctx context.Context, orgID, projectID, periodID uuid.UUID, currency money.Currency) ([]CategorySlice, error)
	Cashflow(ctx context.Context, orgID, projectID uuid.UUID, periodIDs []uuid.UUID, currency money.Currency) ([]CashflowPoint, error)
	Flows(ctx context.Context, orgID, projectID, periodID uuid.UUID, currency money.Currency) ([]Flow, error)
	// WalletBalances reports live balances: Opening is zero, In and Out are all-time, Closing is the derived balance.
	WalletBalances(ctx context.Context, orgID, projectID uuid.UUID) ([]WalletLine, error)
}

// SettingsReader supplies the project's reporting settings; satisfied by the ledger module in boot.
type SettingsReader interface {
	DefaultCurrency(ctx context.Context, orgID, projectID uuid.UUID) (money.Currency, error)
	Location(ctx context.Context, orgID, projectID uuid.UUID) (*time.Location, error)
}
