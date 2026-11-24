package fakes

import (
	"context"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/authz"
	"altalune.id/yasaku/internal/platform/capabilities"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
)

type roleSource struct{ org.Counter }

func (roleSource) CustomPermissions(context.Context, uuid.UUID) ([]authz.Permission, error) {
	return nil, nil
}

// Resolver returns a real authz.Resolver reading roles from store, as boot wires it.
func Resolver(store org.Store) *authz.Resolver {
	return authz.NewResolver(roleSource{org.NewCounter(store, capabilities.Capabilities{})})
}

// Allow returns ctx scoped to tc with a principal holding perms; it skips the role lookup, for tests of a gated service method.
func Allow(ctx context.Context, tc tenant.Context, perms ...authz.Permission) context.Context {
	p := session.PrincipalFrom(ctx)
	p.HeldOrgID = tc.OrgID
	p.Held = make([]string, 0, len(perms))
	for _, h := range perms {
		p.Held = append(p.Held, string(h))
	}
	return session.PrincipalInto(tenant.Into(ctx, tc), p)
}

// Owner returns ctx scoped to tc with a principal holding the built-in owner role and its permissions.
func Owner(ctx context.Context, tc tenant.Context) context.Context {
	return AsRole(ctx, tc, "owner", authz.Builtin("owner")...)
}

// AsRole is Allow with the role the held set came from, as the resolver records it.
func AsRole(ctx context.Context, tc tenant.Context, role string, perms ...authz.Permission) context.Context {
	ctx = Allow(ctx, tc, perms...)
	p := session.PrincipalFrom(ctx)
	p.HeldRole = role
	return session.PrincipalInto(ctx, p)
}
