package org

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/limits"
	"altalune.id/yasaku/internal/platform/audit"
	"altalune.id/yasaku/internal/platform/authz"
	"altalune.id/yasaku/internal/platform/capabilities"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
	customrole "altalune.id/yasaku/internal/role"
	slugs "altalune.id/yasaku/slug"
)

//nolint:gochecknoglobals // OTel tracer is a package-level fixture, not runtime state.
var tracer = otel.Tracer("altalune.id/yasaku/internal/org")

// Service orchestrates org and membership use cases.
type Service struct {
	store      Store
	caps       capabilities.Capabilities
	counter    Counter
	perms      Permissions
	roleNames  RoleNamer
	log        *slog.Logger
	unexpected apperror.UnexpectedFunc
	uow        tenant.UnitOfWork
	limits     Limiter
	audit      Auditor
	onRemoved  []MemberRemovedFunc
}

// Auditor records an org action in the caller's unit of work.
type Auditor interface {
	Record(ctx context.Context, e audit.Event) error
}

// WithAudit records renames and member removals.
func WithAudit(a Auditor) Option {
	return func(s *Service) { s.audit = a }
}

func (s *Service) record(ctx context.Context, e audit.Event) error {
	if s.audit == nil {
		return nil
	}
	if err := s.audit.Record(ctx, e); err != nil {
		return s.unexpected(ctx, "org: audit "+e.Action, err)
	}
	return nil
}

// Limiter locks a subject's row and refuses a create past its limit.
type Limiter interface {
	Lock(ctx context.Context, subs ...limits.Subject) error
	Check(ctx context.Context, key limits.Key, sub limits.Subject, used int64) error
}

// Option configures a Service.
type Option func(*Service)

// Permissions resolves what a role holds, for the escalation guard.
type Permissions interface {
	PermissionsOf(ctx context.Context, role string, roleID uuid.UUID) ([]authz.Permission, error)
}

// RoleNamer names a custom role for a refusal message.
type RoleNamer interface {
	RoleName(ctx context.Context, roleID uuid.UUID) (string, error)
}

// WithRoleNames names custom roles in escalation refusals; without it they read "custom".
func WithRoleNames(n RoleNamer) Option {
	return func(s *Service) { s.roleNames = n }
}

// WithPermissions resolves role permissions through p, custom roles included; without it only built-in roles resolve.
func WithPermissions(p Permissions) Option {
	return func(s *Service) { s.perms = p }
}

// WithLimits holds Create to the creator's org-creation and org-membership limits.
func WithLimits(l Limiter) Option {
	return func(s *Service) { s.limits = l }
}

// MemberRemovedFunc reacts to a member leaving an org, such as revoking what the member held there.
type MemberRemovedFunc func(ctx context.Context, orgID, userID uuid.UUID) error

// OnMemberRemoved registers fn to run on every RemoveMember, before the membership is deleted. NOTE: register at boot, before serving; the list is not guarded for concurrent registration.
func (s *Service) OnMemberRemoved(fn MemberRemovedFunc) { s.onRemoved = append(s.onRemoved, fn) }

// NewService wires the Service.
func NewService(store Store, caps capabilities.Capabilities, log *slog.Logger, unexpected apperror.UnexpectedFunc, uow tenant.UnitOfWork, opts ...Option) *Service {
	s := &Service{store: store, caps: caps, counter: NewCounter(store, caps), log: log, unexpected: unexpected, uow: uow}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// BootstrapSingleton idempotently ensures the one system org exists with an owner membership for ownerID; an existing system org is reused whatever slug is passed, and a blank slug is generated.
func (s *Service) BootstrapSingleton(ctx context.Context, slug, name string, ownerID uuid.UUID) (*Org, error) {
	ctx, span := tracer.Start(ctx, "org.BootstrapSingleton")
	defer span.End()

	// NOTE: bootstrap runs before any tenant exists, but the postgres store needs a tenant scope and
	// the orgs RLS policy is `id = current_setting('app.current_org_id')` — so scope to the org this
	// call will create, and reuse that id when inserting so the row satisfies its own policy.
	ctx, orgID := scopeForBootstrap(ctx, ownerID)

	o, err := s.ensureSingleton(ctx, orgID, strings.TrimSpace(slug), name, ownerID)
	if !IsSystemOrgExistsError(err) && !IsUnreadableExistingOrgError(err) {
		return o, err
	}
	winner, wErr := s.store.SystemOrg(ctx)
	if IsNotFoundError(wErr) && IsUnreadableExistingOrgError(err) {
		return nil, err
	}
	if wErr != nil {
		return nil, s.unexpected(ctx, "org.BootstrapSingleton: SystemOrg after conflict", wErr)
	}
	return s.adoptSingleton(ctx, winner, ownerID)
}

func (s *Service) ensureSingleton(ctx context.Context, orgID uuid.UUID, slug, name string, ownerID uuid.UUID) (*Org, error) {
	existing, err := s.store.SystemOrg(ctx)
	if err == nil {
		return s.adoptSingleton(ctx, existing, ownerID)
	}
	if !IsNotFoundError(err) {
		return nil, s.unexpected(ctx, "org.BootstrapSingleton: SystemOrg", err)
	}

	if slug == "" {
		return slugs.Retry(slugs.MaxAttempts, IsAlreadyExistsError, func(candidate string) (*Org, error) {
			_, err := s.store.BySlug(ctx, candidate)
			if err == nil {
				return nil, &AlreadyExistsError{Slug: candidate}
			}
			if !IsNotFoundError(err) {
				return nil, s.unexpected(ctx, "org.BootstrapSingleton: BySlug", err, "slug", candidate)
			}
			return s.insertSingleton(ctx, orgID, candidate, name, ownerID)
		})
	}

	existing, err = s.store.BySlug(ctx, slug)
	if err == nil {
		return s.adoptSingleton(ctx, existing, ownerID)
	}
	if !IsNotFoundError(err) {
		return nil, s.unexpected(ctx, "org.BootstrapSingleton: BySlug", err, "slug", slug)
	}
	o, err := s.insertSingleton(ctx, orgID, slug, name, ownerID)
	if IsAlreadyExistsError(err) {
		// NOTE: the slug exists but the lookup above could not see it — under RLS that means the row
		// belongs to a different org id, so report it plainly instead of a misleading not-found.
		return nil, &UnreadableExistingOrgError{Slug: slug}
	}
	return o, err
}

// SECURITY: adopting binds the scope to the adopted org's own id; the caller holds the setup token, which is what authorizes joining it as owner.
func (s *Service) adoptSingleton(ctx context.Context, existing *Org, ownerID uuid.UUID) (*Org, error) {
	ctx = tenant.Into(ctx, tenant.Context{OrgID: existing.ID, UserID: ownerID})
	if !existing.System {
		existing.System = true
		if sErr := s.store.Save(ctx, existing); sErr != nil {
			if IsSystemOrgExistsError(sErr) {
				return nil, sErr
			}
			return nil, s.unexpected(ctx, "org.BootstrapSingleton: Save", sErr, "org_id", existing.ID.String())
		}
	}
	m, mErr := s.store.MembershipOf(ctx, existing.ID, ownerID)
	if mErr == nil {
		if !m.System {
			m.System = true
			if sErr := s.store.SaveMembership(ctx, m); sErr != nil {
				return nil, s.unexpected(ctx, "org.BootstrapSingleton: SaveMembership", sErr, "org_id", existing.ID.String())
			}
		}
		return existing, nil
	}
	if !IsMembershipMissingError(mErr) && !IsNotFoundError(mErr) {
		return nil, s.unexpected(ctx, "org.BootstrapSingleton: MembershipOf", mErr, "org_id", existing.ID.String())
	}
	m, mErr = NewMembership(existing.ID, ownerID, RoleOwner)
	if mErr != nil {
		return nil, mErr
	}
	m.System = true
	if mErr := s.store.SaveMembership(ctx, m); mErr != nil {
		return nil, s.unexpected(ctx, "org.BootstrapSingleton: SaveMembership", mErr, "org_id", existing.ID.String())
	}
	return existing, nil
}

func (s *Service) insertSingleton(ctx context.Context, orgID uuid.UUID, slug, name string, ownerID uuid.UUID) (*Org, error) {
	o, err := NewOrg(slug, name, ownerID)
	if err != nil {
		return nil, err
	}
	o.ID = orgID
	o.System = true
	if err := s.store.Save(ctx, o); err != nil {
		if IsAlreadyExistsError(err) || IsSystemOrgExistsError(err) {
			return nil, err
		}
		return nil, s.unexpected(ctx, "org.BootstrapSingleton: Save", err, "slug", slug)
	}
	m, err := NewMembership(o.ID, ownerID, RoleOwner)
	if err != nil {
		return nil, err
	}
	m.System = true
	if err := s.store.SaveMembership(ctx, m); err != nil {
		return nil, s.unexpected(ctx, "org.BootstrapSingleton: SaveMembership", err, "org_id", o.ID.String())
	}
	return o, nil
}

// Create constructs a new org and enrols the owner as an owner-role member; an empty slug is generated.
func (s *Service) Create(ctx context.Context, req CreateRequest) (*Org, error) {
	ctx, span := tracer.Start(ctx, "org.Create")
	defer span.End()

	if !s.caps.OrgCreation {
		return nil, &CreationDisabledError{}
	}

	if chosen := strings.TrimSpace(req.Slug); chosen != "" {
		return s.createWithSlug(ctx, req, chosen)
	}
	return slugs.Retry(slugs.MaxAttempts, IsAlreadyExistsError, func(candidate string) (*Org, error) {
		return s.createWithSlug(ctx, req, candidate)
	})
}

func (s *Service) createWithSlug(ctx context.Context, req CreateRequest, chosen string) (*Org, error) {
	existing, err := s.store.BySlug(ctx, chosen)
	switch {
	case err == nil && existing != nil:
		return nil, &AlreadyExistsError{Slug: chosen}
	case err != nil && !IsNotFoundError(err):
		return nil, s.unexpected(ctx, "org.Create: BySlug", fmt.Errorf("org.Create: BySlug: %w", err), "slug", chosen)
	}

	o, err := NewOrg(chosen, req.Name, req.OwnerID)
	if err != nil {
		return nil, err
	}
	// SECURITY: orgs and memberships are scoped by the org's own id, so the new row's scope is the only one RLS accepts — never the caller's active org.
	ctx = tenant.Into(ctx, tenant.Context{OrgID: o.ID, UserID: req.OwnerID})
	m, err := NewMembership(o.ID, req.OwnerID, RoleOwner)
	if err != nil {
		return nil, err
	}
	err = s.uow(ctx, func(ctx context.Context) error {
		if err := s.checkCreatorLimits(ctx, req.OwnerID, req.Name); err != nil {
			return err
		}
		if err := s.store.Save(ctx, o); err != nil {
			if IsAlreadyExistsError(err) {
				return err
			}
			return s.unexpected(ctx, "org.Create: Save", fmt.Errorf("org.Create: Save: %w", err), "slug", chosen)
		}
		if err := s.store.SaveMembership(ctx, m); err != nil {
			return s.unexpected(ctx, "org.Create: SaveMembership", fmt.Errorf("org.Create: SaveMembership: %w", err), "org_id", o.ID.String(), "user_id", req.OwnerID.String())
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return o, nil
}

// CreateAllowed returns the *limits.ReachedError Create would refuse userID with, read without a lock, so the console can disable "New organization" with its reason.
func (s *Service) CreateAllowed(ctx context.Context, userID uuid.UUID) error {
	if s.limits == nil {
		return nil
	}
	c, err := s.creatorCounts(ctx, userID)
	if err != nil || c.staff {
		return err
	}
	sub := limits.Subject{Kind: limits.SubjectUser, ID: userID}
	if err := s.limits.Check(ctx, limits.UserOrgsCreated, sub, c.created); err != nil {
		return err
	}
	return s.limits.Check(ctx, limits.UserOrgsJoined, sub, c.joined)
}

// IsStaff reports whether userID is platform staff: an owner or admin of the system org, on a deployment with support access. Computed per call, never stored.
func (s *Service) IsStaff(ctx context.Context, userID uuid.UUID) (bool, error) {
	staff, err := s.counter.IsStaff(ctx, userID)
	if err != nil {
		return false, s.unexpected(ctx, "org.IsStaff", err, "user_id", userID.String())
	}
	return staff, nil
}

func (s *Service) checkCreatorLimits(ctx context.Context, userID uuid.UUID, name string) error {
	if s.limits == nil {
		return nil
	}
	sub := limits.Subject{Kind: limits.SubjectUser, ID: userID}
	if err := s.limits.Lock(ctx, sub); err != nil {
		return s.unexpected(ctx, "org.Create: limit lock", err, "user_id", userID.String())
	}
	c, err := s.creatorCounts(ctx, userID)
	if err != nil || c.staff {
		return err
	}
	if err := s.limits.Check(ctx, limits.UserOrgsCreated, sub, c.created); err != nil {
		return err
	}
	err = s.limits.Check(ctx, limits.UserOrgsJoined, sub, c.joined)
	if re, ok := errors.AsType[*limits.ReachedError](err); ok {
		re.Name = name
	}
	return err
}

type creatorCounts struct {
	staff           bool
	created, joined int64
}

func (s *Service) creatorCounts(ctx context.Context, userID uuid.UUID) (creatorCounts, error) {
	staff, err := s.IsStaff(ctx, userID)
	if err != nil || staff {
		return creatorCounts{staff: staff}, err
	}
	created, err := s.store.CountCreatedBy(ctx, userID)
	if err != nil {
		return creatorCounts{}, s.unexpected(ctx, "org.Create: count created", err, "user_id", userID.String())
	}
	joined, err := s.OrgsJoined(ctx, userID)
	if err != nil {
		return creatorCounts{}, err
	}
	return creatorCounts{created: created, joined: joined}, nil
}

// OrgsJoined counts the orgs userID belongs to, the system org aside.
func (s *Service) OrgsJoined(ctx context.Context, userID uuid.UUID) (int64, error) {
	n, err := s.counter.CountOrgsJoined(ctx, userID)
	if err != nil {
		return 0, s.unexpected(ctx, "org.OrgsJoined", err, "user_id", userID.String())
	}
	return n, nil
}

// List returns every org the given user is a member of.
func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]*Org, error) {
	ctx, span := tracer.Start(ctx, "org.List")
	defer span.End()

	orgs, err := s.store.List(ctx, userID)
	if err != nil {
		return nil, s.unexpected(ctx, "org.List", fmt.Errorf("org.List: %w", err), "user_id", userID.String())
	}
	return orgs, nil
}

// Rename loads, mutates and persists an org's display name.
func (s *Service) Rename(ctx context.Context, id uuid.UUID, name string) (*Org, error) {
	ctx, span := tracer.Start(ctx, "org.Rename")
	defer span.End()
	if err := authz.Can(tenant.WithOrg(ctx, id), authz.MembersWrite); err != nil {
		return nil, err
	}

	// SECURITY: the scope must name the org being renamed, not whichever org the caller is active in.
	// Membership in id is the caller's authorization and is checked before this call.
	ctx = tenant.WithOrg(ctx, id)
	o, err := s.store.ByID(ctx, id)
	if err != nil {
		if IsNotFoundError(err) {
			return nil, err
		}
		return nil, s.unexpected(ctx, "org.Rename: ByID", fmt.Errorf("org.Rename: ByID: %w", err), "org_id", id.String())
	}
	if o.System {
		return nil, &SystemProtectedError{Op: "rename", OrgID: id.String(), Resource: "org"}
	}
	from := o.Name
	if err := o.Rename(name); err != nil {
		return nil, err
	}
	err = s.uow(ctx, func(ctx context.Context) error {
		if err := s.store.Save(ctx, o); err != nil {
			return s.unexpected(ctx, "org.Rename: Save", fmt.Errorf("org.Rename: Save: %w", err), "org_id", id.String())
		}
		return s.record(ctx, audit.Event{Action: "org.renamed", TargetKind: "org", TargetID: id.String(),
			Detail: map[string]string{"from_name": from, "to_name": o.Name}})
	})
	if err != nil {
		return nil, err
	}
	return o, nil
}

// AddMember creates a membership row idempotently.
func (s *Service) AddMember(ctx context.Context, orgID, userID uuid.UUID, role Role) (*Membership, error) {
	ctx, span := tracer.Start(ctx, "org.AddMember")
	defer span.End()

	if !role.IsValid() {
		return nil, &InvalidRoleError{Role: string(role)}
	}

	if _, err := s.store.MembershipOf(ctx, orgID, userID); err == nil {
		return nil, &MembershipExistsError{OrgID: orgID.String(), UserID: userID.String()}
	} else if !IsMembershipMissingError(err) && !IsNotFoundError(err) {
		return nil, s.unexpected(ctx, "org.AddMember: MembershipOf", fmt.Errorf("org.AddMember: MembershipOf: %w", err), "org_id", orgID.String(), "user_id", userID.String())
	}

	m, err := NewMembership(orgID, userID, role)
	if err != nil {
		return nil, err
	}
	if err := s.store.SaveMembership(ctx, m); err != nil {
		return nil, s.unexpected(ctx, "org.AddMember: SaveMembership", fmt.Errorf("org.AddMember: SaveMembership: %w", err), "org_id", orgID.String(), "user_id", userID.String())
	}
	return m, nil
}

// RemoveMember deletes a membership row.
func (s *Service) RemoveMember(ctx context.Context, orgID, userID uuid.UUID) error {
	ctx, span := tracer.Start(ctx, "org.RemoveMember")
	defer span.End()
	// SECURITY: the scope must name orgID, not whichever org the caller is active in.
	ctx = tenant.WithOrg(ctx, orgID)
	if err := authz.Can(ctx, authz.MembersWrite); err != nil {
		return err
	}
	m, err := s.removable(ctx, orgID, userID)
	if err != nil {
		return err
	}
	// SECURITY: hooks run before the delete, so a failed revoke leaves the member in place and a retry can finish it.
	for _, fn := range s.onRemoved {
		if err := fn(ctx, orgID, userID); err != nil {
			return s.unexpected(ctx, "org.RemoveMember: hook", fmt.Errorf("org.RemoveMember: hook: %w", err), "org_id", orgID.String(), "user_id", userID.String())
		}
	}
	email, err := s.memberEmail(ctx, orgID, userID)
	if err != nil {
		return s.unexpected(ctx, "org.RemoveMember: profile", err, "org_id", orgID.String(), "user_id", userID.String())
	}
	return s.uow(ctx, func(ctx context.Context) error {
		if err := s.store.LockOrg(ctx, orgID); err != nil {
			return s.unexpected(ctx, "org.RemoveMember: lock", err, "org_id", orgID.String())
		}
		if _, err := s.removable(ctx, orgID, userID); err != nil {
			return err
		}
		if err := s.store.RemoveMember(ctx, orgID, userID); err != nil {
			if IsMembershipMissingError(err) {
				return err
			}
			return s.unexpected(ctx, "org.RemoveMember", fmt.Errorf("org.RemoveMember: %w", err), "org_id", orgID.String(), "user_id", userID.String())
		}
		return s.record(ctx, audit.Event{Action: "member.removed", TargetKind: "user", TargetID: userID.String(),
			Detail: map[string]string{"name": email, "email": email, "role": string(m.Role)}})
	})
}

// SECURITY: refused here rather than in the handler so every transport is gated; it runs again under the org lock, so two removals cannot leave the org ownerless.
func (s *Service) removable(ctx context.Context, orgID, userID uuid.UUID) (*Membership, error) {
	m, err := s.store.MembershipOf(ctx, orgID, userID)
	if err != nil {
		if IsMembershipMissingError(err) || IsNotFoundError(err) {
			return nil, err
		}
		return nil, s.unexpected(ctx, "org.RemoveMember: MembershipOf", err, "org_id", orgID.String(), "user_id", userID.String())
	}
	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	if refusal := RemovalRefusal(orgID, tc.UserID, userID, m.System); refusal != nil {
		return nil, refusal
	}
	covers, err := s.covers(ctx, roleRef{m.Role, m.RoleID})
	if err != nil {
		return nil, err
	}
	if !covers || (m.Role == RoleOwner && !callerIsOwner(ctx, orgID)) {
		return nil, &authz.EscalationError{Role: string(m.Role), Op: authz.EscalationRemove}
	}
	if m.Role == RoleOwner {
		if err := s.requireAnotherOwner(ctx, orgID, userID); err != nil {
			return nil, err
		}
	}
	return m, nil
}

type roleRef struct {
	role Role
	id   uuid.UUID
}

func builtin(r Role) roleRef { return roleRef{role: r} }

func (s *Service) covers(ctx context.Context, roles ...roleRef) (bool, error) {
	held := authz.Held(ctx)
	for _, r := range roles {
		perms, err := s.permissionsOf(ctx, r)
		if err != nil {
			return false, err
		}
		if !authz.Covers(held, perms...) {
			return false, nil
		}
	}
	return true, nil
}

func (s *Service) permissionsOf(ctx context.Context, r roleRef) ([]authz.Permission, error) {
	if r.id == uuid.Nil {
		return authz.Builtin(string(r.role)), nil
	}
	if s.perms == nil {
		return nil, &InvalidRoleError{Role: string(r.role)}
	}
	perms, err := s.perms.PermissionsOf(ctx, string(r.role), r.id)
	if customrole.IsNotFoundError(err) {
		return nil, &InvalidRoleError{Role: string(r.role)}
	}
	if err != nil {
		return nil, s.unexpected(ctx, "org: role permissions", err, "role_id", r.id.String())
	}
	return perms, nil
}

// SECURITY: only a built-in owner makes, demotes or removes an owner; a custom role holding every permission is still not one.
func callerIsOwner(ctx context.Context, orgID uuid.UUID) bool {
	p := session.PrincipalFrom(ctx)
	return p.HeldOrgID == orgID && p.HeldRole == string(RoleOwner)
}

// NOTE: a display label only, so a failed lookup falls back to the bare role kind.
func (s *Service) roleLabel(ctx context.Context, r roleRef) string {
	if r.id == uuid.Nil || s.roleNames == nil {
		return string(r.role)
	}
	if name, err := s.roleNames.RoleName(ctx, r.id); err == nil {
		return name
	}
	return string(r.role)
}

// SECURITY: a custom role always names its role id and a built-in never does, so the guard reads the permissions actually granted.
func validGrant(r Role, roleID uuid.UUID) bool {
	if r == RoleCustom {
		return roleID != uuid.Nil
	}
	return r.IsValid() && roleID == uuid.Nil
}

// SetRole gives userID a built-in role, or the custom role roleID, in orgID. SECURITY: the caller's held set must cover the target's current permissions and the new role's, so nobody grants or strips more than they hold, and only an owner makes an owner.
func (s *Service) SetRole(ctx context.Context, orgID, userID uuid.UUID, role string, roleID uuid.UUID) error {
	ctx, span := tracer.Start(ctx, "org.SetRole")
	defer span.End()
	ctx = tenant.WithOrg(ctx, orgID)
	if err := authz.Can(ctx, authz.MembersWrite); err != nil {
		return err
	}
	next := roleRef{Role(role), roleID}
	if !validGrant(next.role, next.id) {
		return &InvalidRoleError{Role: role}
	}
	m, err := s.settable(ctx, orgID, userID, next)
	if err != nil || (m.Role == next.role && m.RoleID == next.id) {
		return err
	}
	email, err := s.memberEmail(ctx, orgID, userID)
	if err != nil {
		return s.unexpected(ctx, "org.SetRole: profile", err, "org_id", orgID.String(), "user_id", userID.String())
	}
	return s.uow(ctx, func(ctx context.Context) error {
		if err := s.store.LockOrg(ctx, orgID); err != nil {
			return s.unexpected(ctx, "org.SetRole: lock", err, "org_id", orgID.String())
		}
		m, err := s.settable(ctx, orgID, userID, next)
		if err != nil || (m.Role == next.role && m.RoleID == next.id) {
			return err
		}
		updated := *m
		updated.Role, updated.RoleID = next.role, next.id
		if err := s.store.SaveMembership(ctx, &updated); err != nil {
			return s.unexpected(ctx, "org.SetRole: save", err, "org_id", orgID.String(), "user_id", userID.String())
		}
		return s.record(ctx, audit.Event{Action: "member.role_changed", TargetKind: "user", TargetID: userID.String(),
			Detail: map[string]string{"name": email, "email": email, "from_role": string(m.Role), "to_role": role}})
	})
}

// SECURITY: runs before the unit of work and again under the org lock, so a concurrent change cannot slip past the cover or last-owner check.
func (s *Service) settable(ctx context.Context, orgID, userID uuid.UUID, next roleRef) (*Membership, error) {
	m, err := s.store.MembershipOf(ctx, orgID, userID)
	if err != nil {
		if IsMembershipMissingError(err) || IsNotFoundError(err) {
			return nil, err
		}
		return nil, s.unexpected(ctx, "org.SetRole: MembershipOf", err, "org_id", orgID.String(), "user_id", userID.String())
	}
	if m.System {
		return nil, &SystemProtectedError{Op: "set_role", OrgID: orgID.String(), UserID: userID.String(), Resource: "membership"}
	}
	covers, err := s.covers(ctx, roleRef{m.Role, m.RoleID}, next)
	if err != nil {
		return nil, err
	}
	if !covers || ((m.Role == RoleOwner || next.role == RoleOwner) && !callerIsOwner(ctx, orgID)) {
		return nil, &authz.EscalationError{Role: s.roleLabel(ctx, next)}
	}
	if m.Role == RoleOwner && next.role != RoleOwner {
		if err := s.requireAnotherOwner(ctx, orgID, userID); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// MemberControl is what the caller may do to one member, so every surface offers only what would succeed.
type MemberControl struct {
	Member       *MemberProfile
	Removable    bool
	RoleEditable bool
	CanMakeOwner bool
	LastOwner    bool
}

// MemberControls lists orgID's members with what the caller on ctx may change, plus the roles a role picker may offer; owner is never among them, it is granted on its own.
func (s *Service) MemberControls(ctx context.Context, orgID uuid.UUID) ([]MemberControl, []Role, error) {
	ctx = tenant.WithOrg(ctx, orgID)
	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, nil, err
	}
	profiles, err := s.ListMemberProfiles(ctx, orgID)
	if err != nil {
		return nil, nil, err
	}
	manage := authz.Can(ctx, authz.MembersWrite) == nil
	owners := 0
	for _, m := range profiles {
		if m.Role == RoleOwner && !m.System {
			owners++
		}
	}
	var grantable []Role
	makeOwner := false
	if manage {
		for _, r := range []Role{RoleAdmin, RoleMember} {
			if ok, err := s.covers(ctx, builtin(r)); err != nil {
				return nil, nil, err
			} else if ok {
				grantable = append(grantable, r)
			}
		}
		if makeOwner, err = s.covers(ctx, builtin(RoleOwner)); err != nil {
			return nil, nil, err
		}
		makeOwner = makeOwner && callerIsOwner(ctx, orgID)
	}
	out := make([]MemberControl, 0, len(profiles))
	for _, m := range profiles {
		c := MemberControl{Member: m, LastOwner: m.Role == RoleOwner && !m.System && owners == 1}
		if manage && !m.System {
			editable, err := s.covers(ctx, roleRef{m.Role, m.RoleID})
			if err != nil {
				return nil, nil, err
			}
			editable = editable && (m.Role != RoleOwner || callerIsOwner(ctx, orgID))
			c.RoleEditable = editable
			c.Removable = editable && m.UserID != tc.UserID && !c.LastOwner
			c.CanMakeOwner = editable && makeOwner && m.Role != RoleOwner
		}
		out = append(out, c)
	}
	return out, grantable, nil
}

func (s *Service) requireAnotherOwner(ctx context.Context, orgID, userID uuid.UUID) error {
	members, err := s.store.ListMembers(ctx, orgID)
	if err != nil {
		return s.unexpected(ctx, "org: count owners", err, "org_id", orgID.String())
	}
	for _, m := range members {
		if m.Role == RoleOwner && m.UserID != userID {
			return nil
		}
	}
	name := ""
	if o, err := s.store.ByID(ctx, orgID); err == nil {
		name = o.Name
	}
	return &LastOwnerError{OrgID: orgID.String(), Org: name}
}

func (s *Service) memberEmail(ctx context.Context, orgID, userID uuid.UUID) (string, error) {
	profiles, err := s.store.ListMemberProfiles(ctx, orgID)
	if err != nil {
		return "", err
	}
	for _, p := range profiles {
		if p.UserID == userID {
			return p.Email, nil
		}
	}
	return "", nil
}

// SystemOrg returns the singleton org by its system flag, or a NotFoundError when none exists.
func (s *Service) SystemOrg(ctx context.Context) (*Org, error) {
	ctx, span := tracer.Start(ctx, "org.SystemOrg")
	defer span.End()
	o, err := s.store.SystemOrg(ctx)
	if err != nil {
		if IsNotFoundError(err) {
			return nil, err
		}
		return nil, s.unexpected(ctx, "org.SystemOrg", fmt.Errorf("org.SystemOrg: %w", err))
	}
	return o, nil
}

// BySlug looks up an org by its immutable slug.
func (s *Service) BySlug(ctx context.Context, slug string) (*Org, error) {
	ctx, span := tracer.Start(ctx, "org.BySlug")
	defer span.End()
	o, err := s.store.BySlug(ctx, slug)
	if err != nil {
		if IsNotFoundError(err) {
			return nil, err
		}
		return nil, s.unexpected(ctx, "org.BySlug", fmt.Errorf("org.BySlug: %w", err), "slug", slug)
	}
	return o, nil
}

// ByID looks up an org by its aggregate id.
func (s *Service) ByID(ctx context.Context, id uuid.UUID) (*Org, error) {
	ctx, span := tracer.Start(ctx, "org.ByID")
	defer span.End()
	o, err := s.store.ByID(ctx, id)
	if err != nil {
		if IsNotFoundError(err) {
			return nil, err
		}
		return nil, s.unexpected(ctx, "org.ByID", fmt.Errorf("org.ByID: %w", err), "org_id", id.String())
	}
	return o, nil
}

// MembershipOf returns the membership row for (orgID, userID), or a MembershipMissingError.
func (s *Service) MembershipOf(ctx context.Context, orgID, userID uuid.UUID) (*Membership, error) {
	ctx, span := tracer.Start(ctx, "org.MembershipOf")
	defer span.End()
	m, err := s.store.MembershipOf(ctx, orgID, userID)
	if err != nil {
		if IsMembershipMissingError(err) || IsNotFoundError(err) {
			return nil, err
		}
		return nil, s.unexpected(ctx, "org.MembershipOf", fmt.Errorf("org.MembershipOf: %w", err), "org_id", orgID.String(), "user_id", userID.String())
	}
	return m, nil
}

// RequireTenantOrg refuses with *OrgMismatchError when orgID is not the tenant org on ctx.
func RequireTenantOrg(ctx context.Context, orgID uuid.UUID) error {
	tc, err := tenant.From(ctx)
	if err != nil {
		return err
	}
	if tc.OrgID != orgID {
		return &OrgMismatchError{OrgID: orgID.String()}
	}
	return nil
}

// RequireMember refuses with *MembershipMissingError unless userID holds any membership in orgID. SECURITY: ctx must already carry the tenant scope.
func (s *Service) RequireMember(ctx context.Context, orgID, userID uuid.UUID) error {
	ctx, span := tracer.Start(ctx, "org.RequireMember")
	defer span.End()
	if userID == uuid.Nil {
		return &MembershipMissingError{OrgID: orgID.String(), UserID: userID.String()}
	}
	if _, err := s.MembershipOf(ctx, orgID, userID); err != nil {
		if IsNotFoundError(err) {
			return &MembershipMissingError{OrgID: orgID.String(), UserID: userID.String()}
		}
		return err
	}
	return nil
}

// ListMembers returns every membership in the given org.
func (s *Service) ListMembers(ctx context.Context, orgID uuid.UUID) ([]*Membership, error) {
	ctx, span := tracer.Start(ctx, "org.ListMembers")
	defer span.End()

	ms, err := s.store.ListMembers(ctx, orgID)
	if err != nil {
		return nil, s.unexpected(ctx, "org.ListMembers", fmt.Errorf("org.ListMembers: %w", err), "org_id", orgID.String())
	}
	return ms, nil
}

// ListMemberProfiles returns memberships joined with the member's identity fields (email, name).
func (s *Service) ListMemberProfiles(ctx context.Context, orgID uuid.UUID) ([]*MemberProfile, error) {
	ctx, span := tracer.Start(ctx, "org.ListMemberProfiles")
	defer span.End()

	ps, err := s.store.ListMemberProfiles(ctx, orgID)
	if err != nil {
		return nil, s.unexpected(ctx, "org.ListMemberProfiles", fmt.Errorf("org.ListMemberProfiles: %w", err), "org_id", orgID.String())
	}
	return ps, nil
}

func scopeForBootstrap(ctx context.Context, ownerID uuid.UUID) (context.Context, uuid.UUID) {
	if tc, err := tenant.From(ctx); err == nil && tc.OrgID != uuid.Nil {
		return ctx, tc.OrgID
	}
	id := uuid.New()
	return tenant.Into(ctx, tenant.Context{OrgID: id, UserID: ownerID}), id
}
