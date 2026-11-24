package authz

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
)

type roleSource interface {
	RoleOf(ctx context.Context, orgID, userID uuid.UUID) (role string, roleID uuid.UUID, orgName string, err error)
	CustomPermissions(ctx context.Context, roleID uuid.UUID) ([]Permission, error)
	IsStaff(ctx context.Context, userID uuid.UUID) (bool, error)
}

// Resolver computes a principal's held set once, where a request's tenant scope is bound.
type Resolver struct {
	src roleSource
}

// NewResolver binds a Resolver to its role source.
func NewResolver(src roleSource) *Resolver {
	return &Resolver{src: src}
}

// Bind returns ctx scoped to tc, carrying p with its held set for tc.OrgID. SECURITY: the one place a surface turns a principal into permissions.
func (r *Resolver) Bind(ctx context.Context, p session.Principal, tc tenant.Context) (context.Context, error) {
	tctx := tenant.Into(ctx, tc)
	g, err := r.held(tctx, p, tc.OrgID)
	if err != nil {
		return nil, err
	}
	p.Held = make([]string, 0, len(g.perms))
	for _, h := range g.perms {
		p.Held = append(p.Held, string(h))
	}
	p.HeldOrg, p.HeldOrgID, p.HeldRole = g.orgName, tc.OrgID, g.role
	return session.PrincipalInto(tctx, p), nil
}

// PermissionsOf returns what a built-in role, or the custom role roleID, holds.
func (r *Resolver) PermissionsOf(ctx context.Context, role string, roleID uuid.UUID) ([]Permission, error) {
	if roleID == uuid.Nil {
		return Builtin(role), nil
	}
	return r.src.CustomPermissions(ctx, roleID)
}

type grant struct {
	perms   []Permission
	role    string
	orgName string
}

func (r *Resolver) held(ctx context.Context, p session.Principal, orgID uuid.UUID) (grant, error) {
	switch p.Source {
	case session.SourceAPIKey:
		g, err := r.keyRole(ctx, p, orgID)
		g.perms = admitted(g.perms, p.Scopes)
		return g, err
	case session.SourceToken:
		g, err := r.roleOf(ctx, orgID, p.UserID)
		g.perms = admitted(g.perms, p.Scopes)
		return g, err
	default:
		return r.roleOf(ctx, orgID, p.UserID)
	}
}

// SECURITY: a key acts for its creator, so it holds no more than the creator's current role; a legacy key with no recorded creator keeps admin rights minus people and key management.
func (r *Resolver) keyRole(ctx context.Context, p session.Principal, orgID uuid.UUID) (grant, error) {
	if p.KeyOwnerID == uuid.Nil {
		return grant{perms: without(Builtin("admin"), MembersWrite, RolesWrite, APIKeysWrite)}, nil
	}
	return r.roleOf(ctx, orgID, p.KeyOwnerID)
}

func (r *Resolver) roleOf(ctx context.Context, orgID, userID uuid.UUID) (grant, error) {
	if userID == uuid.Nil {
		return grant{}, nil
	}
	role, roleID, name, err := r.src.RoleOf(ctx, orgID, userID)
	if err != nil {
		return grant{}, fmt.Errorf("authz: role of %s: %w", userID, err)
	}
	perms, err := r.PermissionsOf(ctx, role, roleID)
	if err != nil {
		return grant{}, fmt.Errorf("authz: permissions of %s: %w", role, err)
	}
	return grant{perms: perms, role: role, orgName: name}, nil
}

// SECURITY: a token or key holds a permission only when one of its scopes admits it, and never a console permission.
func admitted(role []Permission, scopes []string) []Permission {
	return slices.DeleteFunc(slices.Clone(role), func(p Permission) bool {
		d, ok := Lookup(p)
		return !ok || d.Holder == HolderConsole || !admits(scopes, d.Scope)
	})
}
