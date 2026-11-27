package role

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/limits"
	"altalune.id/yasaku/internal/platform/audit"
	"altalune.id/yasaku/internal/platform/authz"
	"altalune.id/yasaku/internal/platform/tenant"
)

//nolint:gochecknoglobals // OTel tracer is a package-level fixture, not runtime state.
var tracer trace.Tracer = otel.Tracer("altalune.id/yasaku/internal/role")

// Orgs tells whether an org is the staff org, which refuses custom roles.
type Orgs interface {
	IsStaffOrg(ctx context.Context, orgID uuid.UUID) (bool, error)
}

// Limiter locks a subject's row and refuses a create past its limit.
type Limiter interface {
	Lock(ctx context.Context, subs ...limits.Subject) error
	Check(ctx context.Context, key limits.Key, sub limits.Subject, used int64) error
}

// Auditor records an event in the caller's unit of work.
type Auditor interface {
	Record(ctx context.Context, e audit.Event) error
}

// Service is the custom roles driving port.
type Service struct {
	store      Store
	orgs       Orgs
	log        *slog.Logger
	unexpected apperror.UnexpectedFunc
	uow        tenant.UnitOfWork
	limits     Limiter
	orgNames   OrgNamer
	audit      Auditor
	now        func() time.Time
}

// Option configures a Service.
type Option func(*Service)

// OrgNamer names the org a limit refusal shows.
type OrgNamer interface {
	OrgName(ctx context.Context, orgID uuid.UUID) (string, error)
}

// WithLimits holds every Create to the org's custom role limit; names fills the org name a refusal shows.
func WithLimits(l Limiter, names OrgNamer) Option {
	return func(s *Service) { s.limits, s.orgNames = l, names }
}

// WithAudit records every create, update and delete.
func WithAudit(a Auditor) Option { return func(s *Service) { s.audit = a } }

// NewService binds the service to its dependencies.
func NewService(store Store, orgs Orgs, log *slog.Logger, unexpected apperror.UnexpectedFunc, uow tenant.UnitOfWork, opts ...Option) *Service {
	s := &Service{store: store, orgs: orgs, log: log.With("module", "role"), unexpected: unexpected, uow: uow, now: time.Now}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Create adds a custom role to the org on ctx. SECURITY: needs roles:write, and the caller must hold every permission it grants.
func (s *Service) Create(ctx context.Context, name, description string, perms []Permission) (*Role, error) {
	ctx, span := tracer.Start(ctx, "role.Create")
	defer span.End()
	tc, err := s.writable(ctx)
	if err != nil {
		return nil, err
	}
	r, err := New(tc.OrgID, name, description, perms, s.now())
	if err != nil {
		return nil, err
	}
	if !authz.Covers(authz.Held(ctx), r.Permissions...) {
		return nil, &authz.EscalationError{Role: r.Name}
	}
	err = s.uow(ctx, func(ctx context.Context) error {
		if err := s.checkLimit(ctx, tc.OrgID); err != nil {
			return err
		}
		if err := s.save(ctx, r, 0, "role.Create"); err != nil {
			return err
		}
		return s.record(ctx, "role.created", r)
	})
	if err != nil {
		return nil, err
	}
	return r, nil
}

// Update replaces a custom role's name, description and permissions; ifVersion 0 writes unconditionally. SECURITY: needs roles:write, and the caller must hold every permission the role has and gets.
func (s *Service) Update(ctx context.Context, id uuid.UUID, name, description string, perms []Permission, ifVersion int64) (*Role, error) {
	ctx, span := tracer.Start(ctx, "role.Update")
	defer span.End()
	if _, err := s.writable(ctx); err != nil {
		return nil, err
	}
	r, err := s.byID(ctx, id, "role.Update")
	if err != nil {
		return nil, err
	}
	if !authz.Covers(authz.Held(ctx), r.Permissions...) {
		return nil, &authz.EscalationError{Role: r.Name}
	}
	if err := r.Revise(name, description, perms, s.now()); err != nil {
		return nil, err
	}
	if !authz.Covers(authz.Held(ctx), r.Permissions...) {
		return nil, &authz.EscalationError{Role: r.Name}
	}
	err = s.uow(ctx, func(ctx context.Context) error {
		if err := s.save(ctx, r, ifVersion, "role.Update"); err != nil {
			return err
		}
		return s.record(ctx, "role.updated", r)
	})
	if err != nil {
		return nil, err
	}
	r.Version++
	return r, nil
}

// Delete removes a custom role nobody holds. SECURITY: needs roles:write.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	ctx, span := tracer.Start(ctx, "role.Delete")
	defer span.End()
	if _, err := s.writable(ctx); err != nil {
		return err
	}
	return s.uow(ctx, func(ctx context.Context) error {
		r, err := s.byID(ctx, id, "role.Delete")
		if err != nil {
			return err
		}
		if err := s.store.Delete(ctx, id); err != nil {
			if IsInUseError(err) || IsNotFoundError(err) {
				return err
			}
			return s.unexpected(ctx, "role.Delete: delete", err, "role_id", id.String())
		}
		return s.record(ctx, "role.deleted", r)
	})
}

// List returns the org's custom roles by name. SECURITY: needs members:write, which assigning a role takes.
func (s *Service) List(ctx context.Context) ([]*Role, error) {
	ctx, span := tracer.Start(ctx, "role.List")
	defer span.End()
	if err := s.readable(ctx); err != nil {
		return nil, err
	}
	out, err := s.store.List(ctx)
	if err != nil {
		return nil, s.unexpected(ctx, "role.List", err)
	}
	return out, nil
}

// Get returns one custom role. SECURITY: needs members:write, as List does.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Role, error) {
	ctx, span := tracer.Start(ctx, "role.Get")
	defer span.End()
	if err := s.readable(ctx); err != nil {
		return nil, err
	}
	return s.byID(ctx, id, "role.Get")
}

// HeldBy counts, per role id, the members and pending invites holding it, so the console disables a delete that would fail. SECURITY: needs members:write.
func (s *Service) HeldBy(ctx context.Context) (map[uuid.UUID]int, error) {
	ctx, span := tracer.Start(ctx, "role.HeldBy")
	defer span.End()
	if err := s.readable(ctx); err != nil {
		return nil, err
	}
	held, err := s.store.HeldBy(ctx)
	if err != nil {
		return nil, s.unexpected(ctx, "role.HeldBy", err)
	}
	return held, nil
}

// CreateAllowed returns the *limits.ReachedError Create would refuse with, read without a lock, so the console can disable its form.
func (s *Service) CreateAllowed(ctx context.Context) error {
	tc, err := tenant.From(ctx)
	if err != nil {
		return err
	}
	if s.limits == nil {
		return nil
	}
	existing, err := s.store.List(ctx)
	if err != nil {
		return s.unexpected(ctx, "role.CreateAllowed: count", err, "org_id", tc.OrgID.String())
	}
	return s.named(ctx, tc.OrgID, s.limits.Check(ctx, limits.OrgCustomRoles, limits.Subject{Kind: limits.SubjectOrg, ID: tc.OrgID}, int64(len(existing))))
}

func (s *Service) named(ctx context.Context, orgID uuid.UUID, err error) error {
	re, ok := errors.AsType[*limits.ReachedError](err)
	if !ok || s.orgNames == nil {
		return err
	}
	name, nerr := s.orgNames.OrgName(ctx, orgID)
	if nerr != nil {
		return s.unexpected(ctx, "role: org name", nerr, "org_id", orgID.String())
	}
	re.Name = name
	return re
}

func (s *Service) readable(ctx context.Context) error {
	if _, err := tenant.From(ctx); err != nil {
		return err
	}
	return authz.Can(ctx, authz.MembersWrite)
}

func (s *Service) writable(ctx context.Context) (tenant.Context, error) {
	tc, err := tenant.From(ctx)
	if err != nil {
		return tenant.Context{}, err
	}
	if err := authz.Can(ctx, authz.RolesWrite); err != nil {
		return tenant.Context{}, err
	}
	allowed, err := s.Allowed(ctx)
	if err != nil {
		return tenant.Context{}, err
	}
	if !allowed {
		return tenant.Context{}, &SystemOrgError{}
	}
	return tc, nil
}

// Allowed reports whether the caller's org may hold custom roles; the staff org may not.
func (s *Service) Allowed(ctx context.Context) (bool, error) {
	tc, err := tenant.From(ctx)
	if err != nil {
		return false, err
	}
	staff, err := s.orgs.IsStaffOrg(ctx, tc.OrgID)
	if err != nil {
		return false, s.unexpected(ctx, "role: staff org", err, "org_id", tc.OrgID.String())
	}
	return !staff, nil
}

func (s *Service) byID(ctx context.Context, id uuid.UUID, op string) (*Role, error) {
	r, err := s.store.ByID(ctx, id)
	if err != nil {
		if IsNotFoundError(err) {
			return nil, err
		}
		return nil, s.unexpected(ctx, op+": load", err, "role_id", id.String())
	}
	return r, nil
}

func (s *Service) save(ctx context.Context, r *Role, ifVersion int64, op string) error {
	if err := s.store.Save(ctx, r, ifVersion); err != nil {
		if IsAlreadyExistsError(err) || IsNotFoundError(err) || IsStaleVersionError(err) {
			return err
		}
		return s.unexpected(ctx, op+": save", err, "role_id", r.ID.String())
	}
	return nil
}

func (s *Service) checkLimit(ctx context.Context, orgID uuid.UUID) error {
	if s.limits == nil {
		return nil
	}
	sub := limits.Subject{Kind: limits.SubjectOrg, ID: orgID}
	if err := s.limits.Lock(ctx, sub); err != nil {
		return s.unexpected(ctx, "role.Create: limit lock", err, "org_id", orgID.String())
	}
	existing, err := s.store.List(ctx)
	if err != nil {
		return s.unexpected(ctx, "role.Create: count", err, "org_id", orgID.String())
	}
	return s.named(ctx, orgID, s.limits.Check(ctx, limits.OrgCustomRoles, sub, int64(len(existing))))
}

func (s *Service) record(ctx context.Context, action string, r *Role) error {
	if s.audit == nil {
		return nil
	}
	return s.audit.Record(ctx, audit.Event{Action: action, TargetKind: "role", TargetID: r.ID.String(),
		Detail: map[string]string{"name": r.Name}})
}
