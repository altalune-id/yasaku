package controlplane

import (
	"context"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	orgv1 "altalune.id/yasaku/gen/go/org/v1"
	"altalune.id/yasaku/internal/org"
)

// MemberService implements org.v1.MemberService.
type MemberService struct {
	orgs *org.Service
}

// NewMemberService binds the handler to its collaborators.
func NewMemberService(orgs *org.Service) *MemberService {
	return &MemberService{orgs: orgs}
}

// ListMembers returns the members of the principal's org. SECURITY: a person may name only an org they belong to; a key stays on its own.
func (s *MemberService) ListMembers(ctx context.Context, req *connect.Request[orgv1.ListMembersRequest]) (*connect.Response[orgv1.ListMembersResponse], error) {
	tctx, p, err := scopeToOrg(ctx, s.orgs, req.Msg.GetOrg())
	if err != nil {
		return nil, err
	}
	items, err := s.orgs.ListMemberProfiles(tctx, p.ActiveOrgID)
	if err != nil {
		return nil, err
	}
	resp := &orgv1.ListMembersResponse{Members: make([]*orgv1.Member, 0, len(items))}
	for _, m := range items {
		resp.Members = append(resp.Members, &orgv1.Member{
			UserId:   m.UserID.String(),
			Email:    m.Email,
			Name:     m.Name,
			Role:     string(m.Role),
			JoinedAt: timestamppb.New(m.CreatedAt),
		})
	}
	return connect.NewResponse(resp), nil
}
