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
}
