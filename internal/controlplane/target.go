package controlplane

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	apperrorv1 "altalune.id/yasaku/gen/go/apperror/v1"
	yasakuv1 "altalune.id/yasaku/gen/go/yasaku/v1"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/project"
)

type targetResult struct {
	ctx       context.Context
	org       *org.Org
	project   *project.Project
	principal session.Principal
}

// SECURITY: the one place a yasaku Target becomes a tenant scope; a key is pinned to its active org and to the projects it reaches, a person to their memberships.
func scopeToTarget(ctx context.Context, orgs *org.Service, projects *project.Service, t *yasakuv1.Target) (targetResult, *yasakuv1.Needs, error) {
	p, err := principal(ctx)
	if err != nil {
		return targetResult{}, nil, err
	}
	candidates, err := targetOrgs(ctx, orgs, p)
	if err != nil {
		return targetResult{}, nil, err
	}
	o, nd, err := pickOrg(candidates, t.GetOrg())
	if err != nil || nd != nil {
		return targetResult{}, nd, err
	}
	p, err = pinToOrg(p, o)
	if err != nil {
		return targetResult{}, nil, err
	}
	reachable, err := reachableProjects(ctx, projects, p, o, p.ReachesWholeProject)
	if err != nil {
		return targetResult{}, nil, err
	}
	proj, nd, err := pickProject(reachable, defaultProject(t.GetProject(), p, reachable))
	if err != nil || nd != nil {
		return targetResult{}, nd, err
	}
	return targetResult{ctx: bindTo(ctx, p, o.ID, proj.ID), org: o, project: proj, principal: p}, nil, nil
}

// SECURITY: a key sees only its active org, whatever orgs its owner belongs to.
func targetOrgs(ctx context.Context, orgs *org.Service, p session.Principal) ([]*org.Org, error) {
	if p.Source != session.SourceAPIKey {
		return orgs.List(ctx, p.UserID)
	}
	o, err := orgs.ByID(tenant.Into(ctx, tenant.Context{OrgID: p.ActiveOrgID}), p.ActiveOrgID)
	if org.IsNotFoundError(err) {
		return nil, &ScopeNotFoundError{Field: "org", Value: p.ActiveOrgID.String()}
	}
	if err != nil {
		return nil, err
	}
	return []*org.Org{o}, nil
}

// SECURITY: a key's ActiveOrgID is the grant itself, so it is checked and never rewritten; only a person's is moved to the chosen org.
func pinToOrg(p session.Principal, o *org.Org) (session.Principal, error) {
	if p.Source == session.SourceAPIKey {
		if o.ID != p.ActiveOrgID {
			return session.Principal{}, &ScopeNotFoundError{Field: "org", Value: o.Slug}
		}
		return p, nil
	}
	p.ActiveOrgID = o.ID
	return p, nil
}

func reachableProjects(ctx context.Context, projects *project.Service, p session.Principal, o *org.Org, reaches func(orgID, projectID uuid.UUID) bool) ([]*project.Project, error) {
	all, err := projects.List(tenant.Into(ctx, tenant.Context{OrgID: o.ID, UserID: p.UserID}), o.ID)
	if err != nil {
		return nil, err
	}
	out := make([]*project.Project, 0, len(all))
	for _, pr := range all {
		if reaches(o.ID, pr.ID) {
			out = append(out, pr)
		}
	}
	return out, nil
}

func defaultProject(asked string, p session.Principal, reachable []*project.Project) string {
	if strings.TrimSpace(asked) != "" || p.ActiveProjectID == uuid.Nil {
		return asked
	}
	for _, pr := range reachable {
		if pr.ID == p.ActiveProjectID {
			return pr.Slug
		}
	}
	return asked
}

func pickOrg(orgs []*org.Org, q string) (*org.Org, *yasakuv1.Needs, error) {
	q = strings.TrimSpace(q)
	if q != "" {
		matched := matchTarget(orgs, q, func(o *org.Org) (string, string) { return o.Slug, o.Name })
		switch len(matched) {
		case 0:
			return nil, nil, &ScopeNotFoundError{Field: "org", Value: q}
		case 1:
			return matched[0], nil, nil
		default:
			return nil, needs("org", q+" matches more than one org", orgSlugs(matched)...), nil
		}
	}
	switch len(orgs) {
	case 0:
		return nil, needs("org", "you belong to no org"), nil
	case 1:
		return orgs[0], nil, nil
	default:
		return nil, needs("org", "more than one org is available", orgSlugs(orgs)...), nil
	}
}

func pickProject(projects []*project.Project, q string) (*project.Project, *yasakuv1.Needs, error) {
	q = strings.TrimSpace(q)
	if q != "" {
		matched := matchTarget(projects, q, func(p *project.Project) (string, string) { return p.Slug, p.Name })
		switch len(matched) {
		case 0:
			return nil, nil, &ScopeNotFoundError{Field: "project", Value: q}
		case 1:
			return matched[0], nil, nil
		default:
			return nil, needs("project", q+" matches more than one project", projectSlugs(matched)...), nil
		}
	}
	switch len(projects) {
	case 0:
		return nil, needs("project", "no project is available in this org"), nil
	case 1:
		return projects[0], nil, nil
	default:
		return nil, needs("project", "more than one project is available", projectSlugs(projects)...), nil
	}
}

// NOTE: a slug is unique, so an exact slug match wins over any name match.
func matchTarget[T any](rows []T, q string, key func(T) (slug, name string)) []T {
	var byName []T
	for _, r := range rows {
		slug, name := key(r)
		if strings.EqualFold(slug, q) {
			return []T{r}
		}
		if strings.EqualFold(name, q) {
			byName = append(byName, r)
		}
	}
	return byName
}

func orgSlugs(orgs []*org.Org) []string {
	out := make([]string, 0, len(orgs))
	for _, o := range orgs {
		out = append(out, o.Slug)
	}
	slices.Sort(out)
	return out
}

func projectSlugs(projects []*project.Project) []string {
	out := make([]string, 0, len(projects))
	for _, p := range projects {
		out = append(out, p.Slug)
	}
	slices.Sort(out)
	return out
}

// ScopeNotFoundError signals a Target org or project the caller cannot address. SECURITY: an unknown slug and one outside the caller's reach return this same error.
type ScopeNotFoundError struct {
	Field string
	Value string
}

func (e *ScopeNotFoundError) Error() string {
	return "controlplane: " + e.Field + " " + e.Value + " not found"
}

// ToAppError maps ScopeNotFoundError to a NotFound envelope naming the field.
func (e *ScopeNotFoundError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeNotFound,
		strings.ToUpper(e.Field[:1])+e.Field[1:]+" not found",
		codes.NotFound,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeNotFound,
			Meta: map[string]string{"field": e.Field, "value": e.Value},
		},
	)
}

// IsScopeNotFoundError reports whether err's tree contains a *ScopeNotFoundError.
func IsScopeNotFoundError(err error) bool {
	_, ok := errors.AsType[*ScopeNotFoundError](err)
	return ok
}
