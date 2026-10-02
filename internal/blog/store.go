package blog

import (
	"context"

	"github.com/google/uuid"
)

// Store is the driven port.
type Store interface {
	// Save upserts p, returning a *StaleVersionError when a nonzero ifVersion does not match the stored version.
	Save(ctx context.Context, p *Post, ifVersion int) error
	ByID(ctx context.Context, id uuid.UUID) (*Post, error)
	BySlug(ctx context.Context, projectID uuid.UUID, slug string) (*Post, error)
	List(ctx context.Context, orgID, projectID uuid.UUID, opts ListOpts) ([]*Post, error)
	// Delete removes the post, guarded by a nonzero ifVersion the same way Save is.
	Delete(ctx context.Context, id uuid.UUID, ifVersion int) error
	CountByCategory(ctx context.Context, orgID, projectID uuid.UUID) (map[uuid.UUID]int, error)
	CountByTag(ctx context.Context, orgID, projectID uuid.UUID) (map[uuid.UUID]int, error)
}
