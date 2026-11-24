package invite

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/limits"
	"altalune.id/yasaku/internal/platform/audit"
	"altalune.id/yasaku/internal/platform/authz"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/role"
)

//nolint:gochecknoglobals // OTel tracer is a package-level fixture, not runtime state.
var tracer = otel.Tracer("altalune.id/yasaku/internal/invite")

// Service is the invites driving port.
type Service struct {
	store      Store
	send       *SendWorkflow
	accept     *AcceptWorkflow
	enabled    bool
	uow        func(ctx context.Context, fn func(ctx context.Context) error) error
	audit      Auditor
	perms      Permissions
	roleNames  RoleNamer
	log        *slog.Logger
	unexpected apperror.UnexpectedFunc
}

// NewService binds the service to its dependencies; enabled=false blocks Send with InvitesDisabledError.
func NewService(store Store, send *SendWorkflow, accept *AcceptWorkflow, enabled bool, log *slog.Logger, unexpected apperror.UnexpectedFunc, opts ...ServiceOption) *Service {
	s := &Service{
		store:      store,
		send:       send,
		accept:     accept,
		enabled:    enabled,
		uow:        passthrough,
		log:        log.With("module", "invite"),
		unexpected: unexpected,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Enabled reports whether invite issuance is allowed for this deployment.
func (s *Service) Enabled() bool { return s.enabled }

// Send mints an invite, persists it, and emails the raw token; refused with InvitesDisabledError when disabled.
func (s *Service) Send(ctx context.Context, req SendRequest) (*Invite, error) {
	if err := authz.Can(ctx, authz.MembersWrite); err != nil {
		return nil, err
	}
	if !validGrant(req.Role, req.RoleID) {
		return nil, &InvalidRoleError{Role: string(req.Role)}
	}
	perms, err := s.permissionsOf(ctx, req.Role, req.RoleID)
	if err != nil {
		return nil, err
	}
	if !authz.Covers(authz.Held(ctx), perms...) {
		return nil, &authz.EscalationError{Role: s.roleLabel(ctx, req.Role, req.RoleID)}
	}
	ctx, span := tracer.Start(ctx, "invite.Send")
	defer span.End()
	if !s.enabled {
		return nil, &InvitesDisabledError{Reason: "invites require OIDC in selfhosted mode; set YASAKU_OIDC_ISSUER to enable"}
	}
	return s.send.Execute(ctx, req)
}

// NOTE: a display label only, so a failed lookup falls back to the bare role kind.
func (s *Service) roleLabel(ctx context.Context, r Role, roleID uuid.UUID) string {
	if roleID == uuid.Nil || s.roleNames == nil {
		return string(r)
	}
	if name, err := s.roleNames.RoleName(ctx, roleID); err == nil {
		return name
	}
	return string(r)
}

func (s *Service) permissionsOf(ctx context.Context, r Role, roleID uuid.UUID) ([]authz.Permission, error) {
	if roleID == uuid.Nil {
		return authz.Builtin(string(r)), nil
	}
	if s.perms == nil {
		return nil, &InvalidRoleError{Role: string(r)}
	}
	perms, err := s.perms.PermissionsOf(ctx, string(r), roleID)
	if role.IsNotFoundError(err) {
		return nil, &InvalidRoleError{Role: string(r)}
	}
	if err != nil {
		return nil, s.unexpected(ctx, "invite.Send: role permissions", err, "role_id", roleID.String())
	}
	return perms, nil
}

// Seats reports the caller's org seat use for the invites page.
func (s *Service) Seats(ctx context.Context) (Seats, error) {
	ctx, span := tracer.Start(ctx, "invite.Seats")
	defer span.End()
	tc, err := tenant.From(ctx)
	if err != nil {
		return Seats{}, err
	}
	if s.send == nil {
		return Seats{Max: limits.Unlimited}, nil
	}
	return s.send.seats(ctx, tc.OrgID)
}

// SendAllowed returns the *limits.ReachedError Send would refuse the caller's org with, read without a lock, so the console can disable the invite form with its reason.
func (s *Service) SendAllowed(ctx context.Context) error {
	tc, err := tenant.From(ctx)
	if err != nil {
		return err
	}
	if s.send == nil || s.send.limits == nil {
		return nil
	}
	return s.send.allowed(ctx, tc.OrgID)
}

// HasPendingForEmail reports whether email has any pending (non-consumed) invite.
func (s *Service) HasPendingForEmail(ctx context.Context, email string) (bool, error) {
	ctx, span := tracer.Start(ctx, "invite.HasPendingForEmail")
	defer span.End()
	invs, err := s.store.FindPendingForEmail(ctx, email)
	if err != nil {
		return false, s.unexpected(ctx, "invite.HasPendingForEmail", err)
	}
	return len(invs) > 0, nil
}

// Accept redeems a raw token: verifies the invite, finds-or-creates the user, enrols the membership, and marks the invite consumed.
func (s *Service) Accept(ctx context.Context, req AcceptRequest) (*AcceptResult, error) {
	ctx, span := tracer.Start(ctx, "invite.Accept")
	defer span.End()
	return s.accept.Execute(ctx, req)
}

// ListPending returns unused invites in the caller's org.
func (s *Service) ListPending(ctx context.Context) ([]*Invite, error) {
	ctx, span := tracer.Start(ctx, "invite.ListPending")
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	span.SetAttributes(attribute.String("org_id", tc.OrgID.String()))

	out, err := s.store.ListPending(ctx, tc.OrgID)
	if err != nil {
		span.RecordError(err)
		return nil, s.unexpected(ctx, "invite.ListPending", fmt.Errorf("invite.ListPending: %w", err),
			"org_id", tc.OrgID)
	}
	return out, nil
}

// Revoke deletes a pending invite in the caller's org.
func (s *Service) Revoke(ctx context.Context, id uuid.UUID) error {
	if err := authz.Can(ctx, authz.MembersWrite); err != nil {
		return err
	}
	ctx, span := tracer.Start(ctx, "invite.Revoke",
		trace.WithAttributes(attribute.String("invite.id", id.String())))
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return err
	}

	inv, err := s.store.ByID(ctx, id)
	if err != nil {
		span.RecordError(err)
		if IsNotFoundError(err) {
			return err
		}
		return s.unexpected(ctx, "invite.Revoke: byID", fmt.Errorf("invite.Revoke: byID: %w", err), "invite_id", id)
	}
	if inv.OrgID != tc.OrgID {
		return &NotFoundError{ID: id.String()}
	}
	return s.uow(ctx, func(ctx context.Context) error {
		if err := s.store.Delete(ctx, id); err != nil {
			span.RecordError(err)
			if IsNotFoundError(err) {
				return err
			}
			return s.unexpected(ctx, "invite.Revoke: delete", fmt.Errorf("invite.Revoke: delete: %w", err), "invite_id", id)
		}
		if err := record(ctx, s.audit, audit.Event{Action: "invite.revoked", TargetKind: "invite", TargetID: id.String(),
			Detail: map[string]string{"name": inv.Email, "email": inv.Email}}); err != nil {
			return s.unexpected(ctx, "invite.Revoke: audit", err, "invite_id", id)
		}
		return nil
	})
}
