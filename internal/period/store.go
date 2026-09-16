package period

import (
	"context"
	"time"

	"github.com/google/uuid"

	"altalune.id/yasaku/civil"
)

// Store is the driven port.
type Store interface {
	Save(ctx context.Context, p *Period) error
	ByID(ctx context.Context, id uuid.UUID) (*Period, error)
	List(ctx context.Context, orgID, projectID uuid.UUID, opts ListOpts) ([]*Period, error)
	Current(ctx context.Context, orgID, projectID uuid.UUID) (*Period, error)
	Containing(ctx context.Context, orgID, projectID uuid.UUID, d civil.Date) (*Period, error)
	Neighbors(ctx context.Context, orgID, projectID, id uuid.UUID) (prev, next *Period, err error)
	SaveClosing(ctx context.Context, c *Closing) error
	ListClosings(ctx context.Context, orgID, projectID, periodID uuid.UUID) ([]*Closing, error)
}

// LockingStore is the optional Store extension whose read takes a row lock inside the caller's unit of work.
type LockingStore interface {
	ByIDLocked(ctx context.Context, id uuid.UUID) (*Period, error)
}

// SettingsReader supplies the project's cycle settings; satisfied by the ledger module in boot.
type SettingsReader interface {
	Location(ctx context.Context, orgID, projectID uuid.UUID) (*time.Location, error)
	StartDay(ctx context.Context, orgID, projectID uuid.UUID) (int, error)
}

// Snapshotter computes a period's totals; satisfied by the report module in boot.
type Snapshotter interface {
	Snapshot(ctx context.Context, orgID, projectID, periodID uuid.UUID) (Snapshot, error)
}

// UnitOfWork runs fn inside one transaction.
type UnitOfWork func(ctx context.Context, fn func(ctx context.Context) error) error
