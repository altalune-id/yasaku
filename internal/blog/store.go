package blog

import (
	"context"

	"github.com/google/uuid"
)

// Store is the driven port.
type Store interface {
	Save(ctx context.Context, p *Post) error
	ByID(ctx context.Context, id uuid.UUID) (*Post, error)
	List(ctx context.Context, orgID, projectID uuid.UUID, opts ListOpts) ([]*Post, error)
	Delete(ctx context.Context, id uuid.UUID) error
	CountByCategory(ctx context.Context, orgID, projectID uuid.UUID) (map[uuid.UUID]int, error)
	CountByTag(ctx context.Context, orgID, projectID uuid.UUID) (map[uuid.UUID]int, error)
}
