package dataplane

import (
	"context"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/platform/tenant"
)

// Orgs resolves an org slug before any tenant scope exists.
type Orgs interface {
	BySlug(ctx context.Context, slug string) (OrgRef, error)
}

// Projects resolves a project slug inside an org.
type Projects interface {
	BySlug(ctx context.Context, orgID uuid.UUID, slug string) (ProjectRef, error)
}

// OrgRef is the org this surface needs, referenced across the module boundary by id.
type OrgRef struct{ ID uuid.UUID }

// ProjectRef is the project this surface needs, referenced across the module boundary by id.
type ProjectRef struct{ ID uuid.UUID }

type scope struct {
	orgID     uuid.UUID
	projectID uuid.UUID
}

type resolver struct {
	orgs     Orgs
	projects Projects
}

// SECURITY: scope comes from the path via SECURITY DEFINER helpers, and every failure returns the same masked not-found.
func (rs resolver) resolve(ctx context.Context, orgSlug, projectSlug string) (context.Context, scope, error) {
	o, err := rs.orgs.BySlug(ctx, orgSlug)
	if err != nil {
		return ctx, scope{}, &NotFoundError{}
	}
	ctx = tenant.Into(ctx, tenant.Context{OrgID: o.ID})

	p, err := rs.projects.BySlug(ctx, o.ID, projectSlug)
	if err != nil {
		return ctx, scope{}, &NotFoundError{}
	}
	ctx = tenant.WithProject(ctx, p.ID)
	return ctx, scope{orgID: o.ID, projectID: p.ID}, nil
}
