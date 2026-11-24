// Package audit records who did what in an org, in the same transaction as the change it describes.
package audit

import (
	"context"
	"fmt"
	"maps"
	"time"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/reqid"
)

// ActorKind names who acted.
type ActorKind string

const (
	// ActorUser is a person acting in their own org.
	ActorUser ActorKind = "user"
	// ActorStaff is platform staff acting in a support session.
	ActorStaff ActorKind = "staff"
	// ActorAPIKey is an API key.
	ActorAPIKey ActorKind = "api_key"
	// ActorSystem is the platform itself: a job, a scheduler or an operator command.
	ActorSystem ActorKind = "system"
)

// Event is what a caller reports; the org, actor and request come from ctx.
type Event struct {
	Action     string
	TargetKind string
	TargetID   string
	Reason     string
	ProjectID  uuid.UUID
	Detail     map[string]string
}

// Entry is one recorded event.
type Entry struct {
	ID         uuid.UUID
	OrgID      uuid.UUID
	ProjectID  uuid.UUID
	ActorKind  ActorKind
	ActorID    uuid.UUID
	ActorEmail string
	Action     string
	TargetKind string
	TargetID   string
	Reason     string
	RequestID  string
	Detail     map[string]string
	OccurredAt time.Time
}

// Filter pages an org's entries, newest first; Before is the cursor from the last entry of the previous page.
type Filter struct {
	Limit  int
	Before time.Time
}

// DefaultLimit is the page size when Filter.Limit is zero.
const DefaultLimit = 50

//nolint:gochecknoglobals // Immutable allowlist; not runtime state.
var detailKeys = map[string]bool{
	"role": true, "from_role": true, "to_role": true,
	"name": true, "from_name": true, "to_name": true,
	"email": true, "key": true, "value": true, "plan": true,
	"subject_kind": true, "subject_id": true, "kind": true, "scopes": true,
	"timezone": true, "currency": true, "start_day": true,
	"wallets": true, "categories": true, "transactions": true, "periods": true,
}

type store interface {
	insert(ctx context.Context, e Entry) error
	list(ctx context.Context, orgID uuid.UUID, f Filter) ([]Entry, error)
}

// Recorder writes and reads audit entries.
type Recorder struct {
	store store
	now   func() time.Time
}

// Record appends e for the org on ctx, inside the unit of work on ctx. SECURITY: Detail keys off the allowlist are dropped, so a caller cannot log a secret by accident.
func (r *Recorder) Record(ctx context.Context, e Event) error {
	tc, err := tenant.From(ctx)
	if err != nil {
		return err
	}
	if _, ok := db.CurrentTx(ctx); !ok {
		return &NoUnitOfWorkError{Action: e.Action}
	}
	kind, actorID, email := actorOf(session.PrincipalFrom(ctx), tc.OrgID)
	entry := Entry{
		ID:         uuid.New(),
		OrgID:      tc.OrgID,
		ProjectID:  e.ProjectID,
		ActorKind:  kind,
		ActorID:    actorID,
		ActorEmail: email,
		Action:     e.Action,
		TargetKind: e.TargetKind,
		TargetID:   e.TargetID,
		Reason:     e.Reason,
		RequestID:  reqid.FromContext(ctx),
		Detail:     Redact(e.Detail),
		OccurredAt: r.now().UTC(),
	}
	if err := r.store.insert(ctx, entry); err != nil {
		return fmt.Errorf("audit: record %s: %w", e.Action, err)
	}
	return nil
}

// List returns the org on ctx's entries, newest first.
func (r *Recorder) List(ctx context.Context, f Filter) ([]Entry, error) {
	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	if f.Limit <= 0 {
		f.Limit = DefaultLimit
	}
	out, err := r.store.list(ctx, tc.OrgID, f)
	if err != nil {
		return nil, fmt.Errorf("audit: list: %w", err)
	}
	return out, nil
}

// Redact keeps only the allowlisted keys of detail.
func Redact(detail map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range maps.All(detail) {
		if detailKeys[k] {
			out[k] = v
		}
	}
	return out
}

func actorOf(p session.Principal, orgID uuid.UUID) (ActorKind, uuid.UUID, string) {
	switch {
	case p.KeyID != uuid.Nil:
		return ActorAPIKey, p.KeyID, ""
	case p.UserID != uuid.Nil && p.Support != nil && p.Support.OrgID == orgID:
		return ActorStaff, p.UserID, p.Email
	case p.UserID != uuid.Nil:
		return ActorUser, p.UserID, p.Email
	default:
		return ActorSystem, uuid.Nil, ""
	}
}
