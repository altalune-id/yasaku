package opensheetsync

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Outcome is one sync job's result for its link: an empty Err clears the failure streak, a non-empty one extends it. LinkUpdatedAt is the link's updated_at as the job loaded it, so a link re-saved since then is left alone.
type Outcome struct {
	At            time.Time
	Err           string
	LinkUpdatedAt time.Time
}

// Failure is why a push of Version failed: Count spends one of the row's MaxRowAttempts and RetryAfter holds it back, both only while the row is still at Version (0 means any version).
type Failure struct {
	Reason     string
	At         time.Time
	Count      bool
	RetryAfter time.Time
	Version    int64
}

// Backlog is the project's rows still trailing the sheet: Pending will still be pushed, Failing are the pending rows opensheet has refused at least once, GivenUp are refused MaxRowAttempts times and wait for a new edit.
type Backlog struct {
	Pending int64
	Failing int64
	GivenUp int64
}

// Store is the opensheetsync driven port.
type Store interface {
	// SaveLink upserts the project's link; a link row held by another org is *LinkNotFoundError.
	SaveLink(ctx context.Context, l *Link) error
	// LinkByProject returns the project's link, or *LinkNotFoundError.
	LinkByProject(ctx context.Context, orgID, projectID uuid.UUID) (*Link, error)
	// LinkEnabled reports whether the project has an enabled link; no link is false. Inside a unit of work it share-locks the link row on Postgres, so enabling or removing the link waits for the write.
	LinkEnabled(ctx context.Context, orgID, projectID uuid.UUID) (bool, error)
	// ListEnabledLinks returns the org's enabled links, oldest first.
	ListEnabledLinks(ctx context.Context, orgID uuid.UUID) ([]*Link, error)
	// DeleteLink removes the project's link and, by cascade, its sync state; no link is *LinkNotFoundError.
	DeleteLink(ctx context.Context, orgID, projectID uuid.UUID) error
	// SaveOutcome records a job's result on the link without touching its settings, disabling it at DisableAfter failures in a row; disabled is false and nothing changes when the link was re-saved since o.LinkUpdatedAt.
	SaveOutcome(ctx context.Context, orgID, projectID uuid.UUID, o Outcome) (disabled bool, err error)

	// Mark locks refs in LockOrder; any MarkReferencing must come first, so concurrent writers lock rows in one order.
	Mark(ctx context.Context, orgID, projectID uuid.UUID, refs []Ref, at time.Time) error
	// MarkReferencing marks every transaction of the project that names the wallet or category ref points at.
	MarkReferencing(ctx context.Context, orgID, projectID uuid.UUID, ref Ref, at time.Time) (int64, error)
	// MarkAll marks every wallet, category and transaction of the project.
	MarkAll(ctx context.Context, orgID, projectID uuid.UUID, at time.Time) (int64, error)
	// Claim leases one dirty, unleased state row with attempts left and no pending retry_after to token until at+ttl; ok is false otherwise.
	Claim(ctx context.Context, orgID, projectID uuid.UUID, ref Ref, token uuid.UUID, at time.Time, ttl time.Duration) (State, bool, error)
	// Settle records version as synced and frees token's lease; settled is false for a lost lease, dirty true when a newer version waits.
	Settle(ctx context.Context, orgID, projectID uuid.UUID, ref Ref, version int64, token uuid.UUID) (settled, dirty bool, err error)
	// Release frees token's lease after a failed push and records f; a new edit since f.Version is never charged.
	Release(ctx context.Context, orgID, projectID uuid.UUID, ref Ref, token uuid.UUID, f Failure) error
	// ListDirty returns up to limit claimable dirty refs of the project last touched before cutoff, oldest first.
	ListDirty(ctx context.Context, orgID, projectID uuid.UUID, cutoff, at time.Time, limit int) ([]Ref, error)
	// Backlog counts the project's pending, failing and given-up state rows.
	Backlog(ctx context.Context, orgID, projectID uuid.UUID) (Backlog, error)
}
