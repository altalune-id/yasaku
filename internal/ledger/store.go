package ledger

import (
	"context"

	"github.com/google/uuid"
)

// Store is the driven port.
type Store interface {
	Save(ctx context.Context, s *Settings) error
	ByProject(ctx context.Context, orgID, projectID uuid.UUID) (*Settings, error)
}
