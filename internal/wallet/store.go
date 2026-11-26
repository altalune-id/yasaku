package wallet

import (
	"context"

	"github.com/google/uuid"
)

// Store is the driven port.
type Store interface {
	Save(ctx context.Context, w *Wallet) error
	ByID(ctx context.Context, id uuid.UUID) (*Wallet, error)
	List(ctx context.Context, orgID, projectID uuid.UUID, opts ListOpts) ([]*Wallet, error)
	Delete(ctx context.Context, id uuid.UUID) error
	// DeleteAllInProject removes every row of the tenant project on ctx and reports how many went; only a wipe calls it.
	DeleteAllInProject(ctx context.Context) (int64, error)
}
