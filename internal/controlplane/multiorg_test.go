package controlplane_test

import (
	"context"
	"net/http"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	apikeyv1 "altalune.id/yasaku/gen/go/apikey/v1"
	orgv1 "altalune.id/yasaku/gen/go/org/v1"
	"altalune.id/yasaku/gen/go/org/v1/orgv1connect"
	projectv1 "altalune.id/yasaku/gen/go/project/v1"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/platform/authn"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/project"
)

type multiOrgFixture struct {
	h          *harness
	userID     uuid.UUID
	alphaID    uuid.UUID
	alpha      *project.Project
	beta       *project.Project
	gamma      *project.Project
	foreignOrg string
}

func newMultiOrgFixture(t *testing.T) *multiOrgFixture {
	t.Helper()
	return newMultiOrgFixtureActive(t, false)
}

func newMultiOrgFixtureActive(t *testing.T, activeAlpha bool) *multiOrgFixture {
	t.Helper()
	userID := uuid.New()
	alpha, beta, foreign := newOrg(t, userID, "alpha"), newOrg(t, userID, "beta"), newOrg(t, uuid.New(), "gamma")
	p := session.Principal{Source: session.SourceToken, UserID: userID, Email: "a@b"}
	if activeAlpha {
		p.ActiveOrgID = alpha.ID
	}
	h := newHarness(t, p)
	h.saveOrg(t, alpha, userID)
	h.saveOrg(t, beta, userID)
	h.saveOrg(t, foreign, foreign.OwnerID)

	pa, err := project.New(alpha.ID, "home", "Alpha Home")
	require.NoError(t, err)
	require.NoError(t, h.projs.Save(context.Background(), pa))
	pb, err := project.New(beta.ID, "books", "Beta Books")
	require.NoError(t, err)
	require.NoError(t, h.projs.Save(context.Background(), pb))
	pg, err := project.New(foreign.ID, "vault", "Gamma Vault")
	require.NoError(t, err)
	require.NoError(t, h.projs.Save(context.Background(), pg))
	return &multiOrgFixture{h: h, userID: userID, alphaID: alpha.ID, alpha: pa, beta: pb, gamma: pg, foreignOrg: foreign.Slug}
}

func (h *harness) memberClient() orgv1connect.MemberServiceClient {
	return orgv1connect.NewMemberServiceClient(http.DefaultClient, h.server.URL+"/api")
}

func assertOrgChoiceRequired(t *testing.T, err error) {
	t.Helper()
	require.Equal(t, connect.CodeFailedPrecondition, connectCode(err), "err: %v", err)
	d := appErrorDetail(t, err)
	require.Equal(t, apperror.CodeOrgChoiceRequired, d.GetCode())
	require.Equal(t, "alpha,beta", d.GetMeta()["orgs"])
}

func TestListMembers_TokenInTwoOrgs(t *testing.T) {
	f := newMultiOrgFixture(t)
	c := f.h.memberClient()

	_, err := c.ListMembers(context.Background(), withBearerReq(&orgv1.ListMembersRequest{}))
	assertOrgChoiceRequired(t, err)

	resp, err := c.ListMembers(context.Background(), withBearerReq(&orgv1.ListMembersRequest{Org: "alpha"}))
	require.NoError(t, err)
	require.Len(t, resp.Msg.GetMembers(), 1)
	require.Equal(t, f.userID.String(), resp.Msg.GetMembers()[0].GetUserId())

	_, err = c.ListMembers(context.Background(), withBearerReq(&orgv1.ListMembersRequest{Org: f.foreignOrg}))
	require.Equal(t, connect.CodeNotFound, connectCode(err), "a non-member org must read as not found: %v", err)
}

func TestListProjects_TokenInTwoOrgs(t *testing.T) {
	f := newMultiOrgFixture(t)
	c := f.h.projectClient()

	_, err := c.ListProjects(context.Background(), withBearerReq(&projectv1.ListProjectsRequest{}))
	assertOrgChoiceRequired(t, err)

	resp, err := c.ListProjects(context.Background(), withBearerReq(&projectv1.ListProjectsRequest{Org: "beta"}))
	require.NoError(t, err)
	require.Len(t, resp.Msg.GetProjects(), 1)
	require.Equal(t, f.beta.ID.String(), resp.Msg.GetProjects()[0].GetId())

	resp, err = c.ListProjects(context.Background(), withBearerReq(&projectv1.ListProjectsRequest{Org: f.alphaID.String()}))
	require.NoError(t, err, "org also takes an id")
	require.Len(t, resp.Msg.GetProjects(), 1)
	require.Equal(t, f.alpha.ID.String(), resp.Msg.GetProjects()[0].GetId())

	_, err = c.ListProjects(context.Background(), withBearerReq(&projectv1.ListProjectsRequest{Org: f.foreignOrg}))
	require.Equal(t, connect.CodeNotFound, connectCode(err), "a non-member org must read as not found: %v", err)
}

func TestScopeToProject_TokenResolvesSecondOrg(t *testing.T) {
	f := newMultiOrgFixture(t)
	_, err := f.h.apikeyClient().List(context.Background(), withBearerReq(&apikeyv1.ListRequest{ProjectId: f.beta.ID.String()}))
	require.NoError(t, err, "a token in two orgs resolves the project's org from its memberships")
}

func TestScopeToProject_KeyNeverLeavesActiveOrg(t *testing.T) {
	f := newMultiOrgFixture(t)
	key := f.h.mintKey(f.alphaID, uuid.Nil, []string{authn.ScopeAPIKeysRead}, nil)
	req := connect.NewRequest(&apikeyv1.ListRequest{ProjectId: f.beta.ID.String()})
	withKey(key)(req.Header())
	_, err := f.h.apikeyClient().List(context.Background(), req)
	require.Equal(t, connect.CodeNotFound, connectCode(err), "a key must never resolve a project outside its own org: %v", err)
}

func TestScopeToProject_ActiveOrgStillReachesSecondMembership(t *testing.T) {
	f := newMultiOrgFixtureActive(t, true)
	_, err := f.h.apikeyClient().List(context.Background(), withBearerReq(&apikeyv1.ListRequest{ProjectId: f.beta.ID.String()}))
	require.NoError(t, err, "a person pinned to alpha still reaches beta, an org they belong to")
}

// SECURITY: a project in an org the person does not belong to reads as absent, with or without an active org.
func TestScopeToProject_RefusesNonMemberOrg(t *testing.T) {
	for name, active := range map[string]bool{"no active org": false, "active org set": true} {
		t.Run(name, func(t *testing.T) {
			f := newMultiOrgFixtureActive(t, active)
			_, err := f.h.apikeyClient().List(context.Background(), withBearerReq(&apikeyv1.ListRequest{ProjectId: f.gamma.ID.String()}))
			require.Equal(t, connect.CodeNotFound, connectCode(err), "err: %v", err)
		})
	}
}

func withBearerReq[T any](msg *T) *connect.Request[T] {
	req := connect.NewRequest(msg)
	withBearer(req.Header())
	return req
}
