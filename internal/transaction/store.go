package transaction

import (
	"context"
	"time"

	"github.com/google/uuid"

	"altalune.id/yasaku/money"
)

// Store is the driven port.
type Store interface {
	Save(ctx context.Context, t *Transaction) error
	ByID(ctx context.Context, id uuid.UUID) (*Transaction, error)
	List(ctx context.Context, orgID, projectID uuid.UUID, opts ListOpts) ([]*Transaction, error)
	Delete(ctx context.Context, id uuid.UUID) error
	Balances(ctx context.Context, orgID, projectID uuid.UUID) (map[uuid.UUID]money.Amount, error)
	Balance(ctx context.Context, orgID, projectID, walletID uuid.UUID) (money.Amount, error)
	LastCategoryForNote(ctx context.Context, orgID, projectID uuid.UUID, noteNorm string) (uuid.UUID, bool, error)
	// TopCategories counts income and expense entries per category since since, most used first.
	TopCategories(ctx context.Context, orgID, projectID uuid.UUID, since time.Time, limit int) ([]CategoryCount, error)
	// RecentByCategory returns the newest entries of a category, one per (note, amount), newest first.
	RecentByCategory(ctx context.Context, orgID, projectID, categoryID uuid.UUID, limit int) ([]RecentEntry, error)
	// LockWallet excludes other LockWallet holders for one wallet until the caller's transaction ends. SECURITY: it errors when no transaction is open, because a lock released immediately is no lock at all.
	LockWallet(ctx context.Context, orgID, projectID, walletID uuid.UUID) error
}
