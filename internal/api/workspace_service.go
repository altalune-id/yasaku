package api

import (
	"context"
	"time"

	"connectrpc.com/connect"

	"altalune.id/yasaku/civil"
	yasakuv1 "altalune.id/yasaku/gen/go/yasaku/v1"
	"altalune.id/yasaku/internal/ledger"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/period"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/project"
)

// WorkspaceService implements yasaku.v1.WorkspaceService.
type WorkspaceService struct {
	scope    scopeResolver
	orgs     *org.Service
	projects *project.Service
	ledgers  *ledger.Service
	periods  *period.Service
	now      func() time.Time
}

// NewWorkspaceService binds the handler to its collaborators.
func NewWorkspaceService(orgs *org.Service, projects *project.Service, ledgers *ledger.Service, periods *period.Service) *WorkspaceService {
	return &WorkspaceService{
		scope:    scopeResolver{orgs: orgs, projects: projects},
		orgs:     orgs,
		projects: projects,
		ledgers:  ledgers,
		periods:  periods,
		now:      time.Now,
	}
}

// ListProjects returns every org/project pair the caller may address in a Target.
func (s *WorkspaceService) ListProjects(ctx context.Context, _ *connect.Request[yasakuv1.ListProjectsRequest]) (*connect.Response[yasakuv1.ListProjectsResponse], error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	orgs, err := s.orgs.List(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	out := &yasakuv1.ListProjectsResponse{}
	for _, o := range orgs {
		orgCtx := tenant.Into(ctx, tenant.Context{OrgID: o.ID, UserID: p.UserID})
		projects, lErr := s.projects.List(orgCtx, o.ID)
		if lErr != nil {
			return nil, lErr
		}
		for _, pr := range projects {
			out.Projects = append(out.Projects, &yasakuv1.Project{
				Org:         o.Slug,
				OrgName:     o.Name,
				Project:     pr.Slug,
				ProjectName: pr.Name,
			})
		}
	}
	return connect.NewResponse(out), nil
}

// Now reports the project's clock: its timezone, today's civil date and the open period.
func (s *WorkspaceService) Now(ctx context.Context, req *connect.Request[yasakuv1.NowRequest]) (*connect.Response[yasakuv1.NowResponse], error) {
	sc, nd, err := s.scope.resolve(ctx, req.Msg.GetTarget())
	if err != nil {
		return nil, err
	}
	if nd != nil {
		return nil, needsErr(nd)
	}
	settings, err := s.ledgers.Get(sc.ctx)
	if err != nil {
		return nil, err
	}
	loc, err := settings.Location()
	if err != nil {
		return nil, err
	}
	now := s.now()
	out := &yasakuv1.NowResponse{
		Timezone: settings.Timezone,
		Today:    civil.DateOf(now, loc).String(),
		Now:      toTimestamp(now.UTC()),
	}
	cur, err := s.periods.Current(sc.ctx)
	switch {
	case err == nil:
		out.CurrentPeriod = toProtoPeriod(cur)
	case period.IsNotFoundError(err):
	default:
		return nil, err
	}
	return connect.NewResponse(out), nil
}
