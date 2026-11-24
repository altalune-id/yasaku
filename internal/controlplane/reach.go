package controlplane

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	apperrorv1 "altalune.id/yasaku/gen/go/apperror/v1"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/authz"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/project"
)

// SECURITY: a key principal carries UserID == uuid.Nil, so it is admitted on ActiveOrgID instead.
func principal(ctx context.Context) (session.Principal, error) {
	p := session.PrincipalFrom(ctx)
	usable := p.UserID != uuid.Nil
	if p.Source == session.SourceAPIKey {
		usable = p.ActiveOrgID != uuid.Nil
	}
	if !usable {
		return session.Principal{}, apperror.New(
			apperror.CodeUnauthenticated,
			"No principal in context",
			codes.Unauthenticated,
			&apperrorv1.ErrorDetail{Code: apperror.CodeUnauthenticated},
		)
	}
	return p, nil
}

// Scope binds a request's tenant scope: Orgs resolves the org, Authz resolves what the caller holds in it.
type Scope struct {
	Orgs  *org.Service
	Authz *authz.Resolver
}

type orgLister interface {
	List(ctx context.Context, userID uuid.UUID) ([]*org.Org, error)
}

// SECURITY: the one place a handler turns a caller-supplied project id into a tenant scope; reach is decided by session.Principal alone, and a person's project outside the active org is found only through their memberships.
func scopeToProject(ctx context.Context, sc Scope, projects *project.Service, projectIDRaw string) (context.Context, *project.Project, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, nil, err
	}
	pid, err := parseUUID("project_id", projectIDRaw)
	if err != nil {
		return nil, nil, err
	}
	proj, err := projectInOrg(ctx, projects, p, p.ActiveOrgID, pid)
	if p.Source != session.SourceAPIKey && project.IsNotFoundError(err) {
		proj, err = projectInMemberships(ctx, sc.Orgs, projects, p, pid)
		if err == nil {
			p.ActiveOrgID = proj.OrgID
		}
	}
	if err != nil {
		return nil, nil, err
	}
	if !p.ReachesWholeProject(proj.OrgID, proj.ID) {
		return nil, nil, forbiddenErr("project is outside this credential's reach", "project_id", pid.String())
	}
	bound, err := bindTo(ctx, sc, p, proj.OrgID, proj.ID)
	if err != nil {
		return nil, nil, err
	}
	return bound, proj, nil
}

func projectInOrg(ctx context.Context, projects *project.Service, p session.Principal, orgID, pid uuid.UUID) (*project.Project, error) {
	if orgID == uuid.Nil {
		return nil, &project.NotFoundError{ID: pid.String()}
	}
	return projects.ByID(tenant.Into(ctx, tenant.Context{OrgID: orgID, UserID: p.UserID}), pid)
}

// SECURITY: a key is pinned to its own org and never reaches here; a person reaches only the orgs they belong to.
func projectInMemberships(ctx context.Context, orgs orgLister, projects *project.Service, p session.Principal, pid uuid.UUID) (*project.Project, error) {
	memberships, err := orgs.List(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	for _, o := range memberships {
		if o.ID == p.ActiveOrgID {
			continue
		}
		proj, err := projectInOrg(ctx, projects, p, o.ID, pid)
		if project.IsNotFoundError(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if proj.OrgID == o.ID {
			return proj, nil
		}
	}
	return nil, &project.NotFoundError{ID: pid.String()}
}

// SECURITY: resolves the org a list read is scoped to: a key keeps its own org, a person names one of their memberships or has exactly one in play.
func scopeToOrg(ctx context.Context, sc Scope, orgQuery string) (context.Context, session.Principal, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, session.Principal{}, err
	}
	if strings.TrimSpace(orgQuery) == "" && (p.ActiveOrgID != uuid.Nil || p.Source == session.SourceAPIKey) {
		bound, err := sc.Authz.Bind(ctx, p, tenant.Context{OrgID: p.ActiveOrgID, UserID: p.UserID})
		return bound, p, err
	}
	candidates, err := targetOrgs(ctx, sc.Orgs, p)
	if err != nil {
		return nil, session.Principal{}, err
	}
	o, err := pickListOrg(candidates, orgQuery)
	if err != nil {
		return nil, session.Principal{}, err
	}
	p, err = pinToOrg(p, o)
	if err != nil {
		return nil, session.Principal{}, err
	}
	bound, err := sc.Authz.Bind(ctx, p, tenant.Context{OrgID: o.ID, UserID: p.UserID})
	return bound, p, err
}

func pickListOrg(candidates []*org.Org, q string) (*org.Org, error) {
	q = strings.TrimSpace(q)
	if q != "" {
		for _, o := range candidates {
			if o.ID.String() == q {
				return o, nil
			}
		}
		matched := matchTarget(candidates, q, func(o *org.Org) (string, string) { return o.Slug, o.Name })
		if len(matched) == 1 {
			return matched[0], nil
		}
		if len(matched) > 1 {
			return nil, &OrgChoiceRequiredError{Orgs: orgSlugs(matched)}
		}
		return nil, &ScopeNotFoundError{Field: "org", Value: q}
	}
	switch len(candidates) {
	case 0:
		return nil, &ScopeNotFoundError{Field: "org", Value: q}
	case 1:
		return candidates[0], nil
	default:
		return nil, &OrgChoiceRequiredError{Orgs: orgSlugs(candidates)}
	}
}

// NOTE: an omitted project id falls back to the principal's active project and still passes scopeToProject.
func scopeToActiveProject(ctx context.Context, sc Scope, projects *project.Service, projectIDRaw string) (context.Context, *project.Project, error) {
	if strings.TrimSpace(projectIDRaw) != "" {
		return scopeToProject(ctx, sc, projects, projectIDRaw)
	}
	p, err := principal(ctx)
	if err != nil {
		return nil, nil, err
	}
	if p.ActiveProjectID == uuid.Nil {
		return nil, nil, &ProjectUnresolvedError{}
	}
	return scopeToProject(ctx, sc, projects, p.ActiveProjectID.String())
}

// SECURITY: binds ctx to a loaded row's project only when the principal reaches that row; deny is the row's own out-of-reach answer, so each entity keeps its absent-or-forbidden contract.
func scopeToResource(ctx context.Context, sc Scope, orgID, projectID, resourceID uuid.UUID, deny func() error) (context.Context, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if !p.ReachesResource(orgID, projectID, resourceID) {
		return nil, deny()
	}
	return bindTo(ctx, sc, p, orgID, projectID)
}

// SECURITY: the control plane's one bind; the resolver sets the caller's held set for orgID, so every gated service reads it.
func bindTo(ctx context.Context, sc Scope, p session.Principal, orgID, projectID uuid.UUID) (context.Context, error) {
	return sc.Authz.Bind(ctx, p, tenant.Context{OrgID: orgID, ProjectID: projectID, UserID: p.UserID})
}
