package tag

import (
	"context"

	"github.com/google/uuid"
)

// Store is the driven port.
type Store interface {
	Save(ctx context.Context, t *Tag) error
	ByID(ctx context.Context, id uuid.UUID) (*Tag, error)
	ByIDs(ctx context.Context, orgID, projectID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]*Tag, error)
	BySlug(ctx context.Context, orgID, projectID uuid.UUID, slug string) (*Tag, error)
	List(ctx context.Context, orgID, projectID uuid.UUID) ([]*Tag, error)
	Delete(ctx context.Context, id uuid.UUID) error
}
