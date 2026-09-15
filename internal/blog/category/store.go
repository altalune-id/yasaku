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
	List(ctx context.Context, orgID, projectID uuid.UUID) ([]*Category, error)
	Delete(ctx context.Context, id uuid.UUID) error
}
