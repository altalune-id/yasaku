package category

import (
	"context"

	"github.com/google/uuid"
)

// Store is the driven port.
type Store interface {
	Save(ctx context.Context, c *Category) error
	ByID(ctx context.Context, id uuid.UUID) (*Category, error)
	ByIDs(ctx context.Context, orgID, projectID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]*Category, error)
	List(ctx context.Context, orgID, projectID uuid.UUID, opts ListOpts) ([]*Category, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

// Namer is the driven port resolving a default category's i18n message id to a display name.
type Namer interface {
	DefaultName(ctx context.Context, key string) string
}
