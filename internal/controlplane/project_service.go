package controlplane

import (
	"context"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	projectv1 "altalune.id/yasaku/gen/go/project/v1"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/project"
)

// ProjectService implements project.v1.ProjectService.
type ProjectService struct {
	orgs     *org.Service
	projects *project.Service
}

// NewProjectService binds the handler to its collaborators.
func NewProjectService(orgs *org.Service, projects *project.Service) *ProjectService {
	return &ProjectService{orgs: orgs, projects: projects}
}

// ListProjects returns the projects in the principal's org that the principal reaches. SECURITY: a person may name only an org they belong to; a key stays on its own.
func (s *ProjectService) ListProjects(ctx context.Context, req *connect.Request[projectv1.ListProjectsRequest]) (*connect.Response[projectv1.ListProjectsResponse], error) {
	tctx, p, err := scopeToOrg(ctx, s.orgs, req.Msg.GetOrg())
	if err != nil {
		return nil, err
	}
	items, err := s.projects.List(tctx, p.ActiveOrgID)
	if err != nil {
		return nil, err
	}
	resp := &projectv1.ListProjectsResponse{Projects: make([]*projectv1.Project, 0, len(items))}
	for _, item := range items {
		if !p.ReachesProject(item.OrgID, item.ID) {
			continue
		}
		resp.Projects = append(resp.Projects, projectToProto(item))
	}
	return connect.NewResponse(resp), nil
}

func projectToProto(p *project.Project) *projectv1.Project {
	return &projectv1.Project{
		Id:        p.ID.String(),
		OrgId:     p.OrgID.String(),
		Slug:      p.Slug,
		Name:      p.Name,
		System:    p.System,
		CreatedAt: timestamppb.New(p.CreatedAt),
	}
}
