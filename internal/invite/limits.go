package invite

import (
	"context"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/limits"
	"altalune.id/yasaku/internal/platform/audit"
	"altalune.id/yasaku/internal/platform/authz"
)

// Limiter locks a subject's row and refuses a create past its limit.
type Limiter interface {
	Lock(ctx context.Context, subs ...limits.Subject) error
	Check(ctx context.Context, key limits.Key, sub limits.Subject, used int64) error
	Effective(ctx context.Context, key limits.Key, sub limits.Subject) (int64, error)
}

// Seats is an org's seat use: members plus pending unexpired invites against the org.members limit.
type Seats struct {
	Used    int64
	Max     int64
	Pending int64
}

// Unlimited reports whether the org has no seat limit.
func (s Seats) Unlimited() bool { return s.Max == limits.Unlimited }

// MemberCounter counts what the membership limits measure and names the org a seat refusal shows.
type MemberCounter interface {
	CountMembers(ctx context.Context, orgID uuid.UUID) (int64, error)
	CountOrgsJoined(ctx context.Context, userID uuid.UUID) (int64, error)
	IsStaff(ctx context.Context, userID uuid.UUID) (bool, error)
	OrgName(ctx context.Context, orgID uuid.UUID) (string, error)
}

// WithSendUnitOfWork saves each invite in a unit of work, under the org's seat lock.
func WithSendUnitOfWork(uow func(ctx context.Context, fn func(ctx context.Context) error) error) SendOption {
	return func(w *SendWorkflow) { w.uow = uow }
}

// WithSendLimits holds Send to the org's seats: members plus pending unexpired invites.
func WithSendLimits(l Limiter, c MemberCounter) SendOption {
	return func(w *SendWorkflow) { w.limits, w.counter = l, c }
}

// WithAcceptLimits holds Accept to the acceptor's org-membership limit and the org's seats.
func WithAcceptLimits(l Limiter, c MemberCounter) AcceptOption {
	return func(w *AcceptWorkflow) { w.limits, w.counter = l, c }
}

// Auditor records an invite action in the caller's unit of work.
type Auditor interface {
	Record(ctx context.Context, e audit.Event) error
}

// WithSendAudit records each sent invite with it.
func WithSendAudit(a Auditor) SendOption {
	return func(w *SendWorkflow) { w.audit = a }
}

// WithAcceptAudit records each accepted invite as member.joined.
func WithAcceptAudit(a Auditor) AcceptOption {
	return func(w *AcceptWorkflow) { w.audit = a }
}

// ServiceOption configures a Service.
type ServiceOption func(*Service)

// Permissions resolves what a built-in or custom role holds.
type Permissions interface {
	PermissionsOf(ctx context.Context, role string, roleID uuid.UUID) ([]authz.Permission, error)
}

// RoleNamer names a custom role for a refusal message.
type RoleNamer interface {
	RoleName(ctx context.Context, roleID uuid.UUID) (string, error)
}

// WithRoleNames names custom roles in escalation refusals; without it they read "custom".
func WithRoleNames(n RoleNamer) ServiceOption { return func(s *Service) { s.roleNames = n } }

// WithPermissions resolves custom roles for Send's cover check; without it only built-in roles can be sent.
func WithPermissions(p Permissions) ServiceOption { return func(s *Service) { s.perms = p } }

// WithRevokeAudit deletes each revoked invite in uow and records invite.revoked.
func WithRevokeAudit(uow func(ctx context.Context, fn func(ctx context.Context) error) error, a Auditor) ServiceOption {
	return func(s *Service) { s.uow, s.audit = uow, a }
}

func record(ctx context.Context, a Auditor, e audit.Event) error {
	if a == nil {
		return nil
	}
	return a.Record(ctx, e)
}

func passthrough(ctx context.Context, fn func(ctx context.Context) error) error { return fn(ctx) }
