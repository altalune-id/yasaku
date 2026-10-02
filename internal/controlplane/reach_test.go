package controlplane_test

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	apikeyv1 "altalune.id/yasaku/gen/go/apikey/v1"
	projectv1 "altalune.id/yasaku/gen/go/project/v1"
	yasakuv1 "altalune.id/yasaku/gen/go/yasaku/v1"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/authn"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/project"
)

type reachFixture struct {
	h            *harness
	userID       uuid.UUID
	orgID        uuid.UUID
	own, sibling *project.Project
}

// SECURITY: the fakes filter by org only, so the handler's reach check is the sole guard these tests exercise.
func newReachFixture(t *testing.T) *reachFixture {
	t.Helper()
	ctx := context.Background()
	userID := uuid.New()
	o := newOrg(t, userID, "acme")
	h := newHarness(t, session.Principal{UserID: userID, Email: "a@b", ActiveOrgID: o.ID})
	h.saveOrg(t, o, userID)

	own, err := project.New(o.ID, "own", "Own")
	require.NoError(t, err)
	require.NoError(t, h.projs.Save(ctx, own))
	sibling, err := project.New(o.ID, "sibling", "Sibling")
	require.NoError(t, err)
	require.NoError(t, h.projs.Save(ctx, sibling))

	return &reachFixture{h: h, userID: userID, orgID: o.ID, own: own, sibling: sibling}
}

func newOrg(t *testing.T, owner uuid.UUID, slug string) *org.Org {
	t.Helper()
	o, err := org.NewOrg(slug, slug, owner)
	require.NoError(t, err)
	return o
}

func (h *harness) saveOrg(t *testing.T, o *org.Org, owner uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, h.orgs.Save(ctx, o))
	m, err := org.NewMembership(o.ID, owner, org.RoleOwner)
	require.NoError(t, err)
	require.NoError(t, h.orgs.SaveMembership(ctx, m))
}

func (f *reachFixture) key(scopes []string, resourceIDs ...uuid.UUID) string {
	return f.h.mintKey(f.orgID, f.own.ID, scopes, resourceIDs)
}

func allScopes() []string {
	return []string{authn.ScopeYasakuRead, authn.ScopeYasakuWrite, authn.ScopeAPIKeysRead, authn.ScopeAPIKeysWrite}
}

func target(org, project string) *yasakuv1.Target {
	return &yasakuv1.Target{Org: org, Project: project}
}

// TestKeyNeverReachesASiblingProject pins a key to the project it was minted in. SECURITY: every row names a verb that took a sibling project; revert the reach check and each one fails.
func TestKeyNeverReachesASiblingProject(t *testing.T) {
	f := newReachFixture(t)
	key := f.key(allScopes())
	sibling := f.sibling.ID.String()

	tests := []struct {
		name string
		call func(context.Context) error
		want connect.Code
	}{
		{"GetSettingsBySlug", func(ctx context.Context) error {
			req := connect.NewRequest(&yasakuv1.GetSettingsRequest{Target: target("", "sibling")})
			withKey(key)(req.Header())
			_, err := f.h.ledgerClient().GetSettings(ctx, req)
			return err
		}, connect.CodeNotFound},
		{"GetSettingsByName", func(ctx context.Context) error {
			req := connect.NewRequest(&yasakuv1.GetSettingsRequest{Target: target("acme", "Sibling")})
			withKey(key)(req.Header())
			_, err := f.h.ledgerClient().GetSettings(ctx, req)
			return err
		}, connect.CodeNotFound},
		{"UpdateSettings", func(ctx context.Context) error {
			req := connect.NewRequest(&yasakuv1.UpdateSettingsRequest{Target: target("", "sibling"), Timezone: proto.String("Asia/Jakarta")})
			withKey(key)(req.Header())
			_, err := f.h.ledgerClient().UpdateSettings(ctx, req)
			return err
		}, connect.CodeNotFound},
		{"APIKeyCreate", func(ctx context.Context) error {
			req := connect.NewRequest(&apikeyv1.CreateRequest{
				ExpiresAt: timestamppb.New(time.Now().Add(24 * time.Hour)), ProjectId: sibling, Name: "escalate", Scopes: []string{authn.ScopeYasakuRead}})
			withKey(key)(req.Header())
			_, err := f.h.apikeyClient().Create(ctx, req)
			return err
		}, connect.CodePermissionDenied},
		{"APIKeyList", func(ctx context.Context) error {
			req := connect.NewRequest(&apikeyv1.ListRequest{ProjectId: sibling})
			withKey(key)(req.Header())
			_, err := f.h.apikeyClient().List(ctx, req)
			return err
		}, connect.CodePermissionDenied},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call(t.Context())
			require.Error(t, err, "a key minted for one project reached its sibling")
			require.Equal(t, tt.want, connectCode(err), "err=%v", err)
		})
	}
	require.Zero(t, f.h.ledger.Len(), "the sibling ledger settings must be left untouched")
}

// SECURITY: the owner belongs to both orgs, so a resolver that read the owner's memberships would let the key into the second one.
func TestKeyNeverReachesAnotherOrgOfItsOwner(t *testing.T) {
	f := newReachFixture(t)
	beta := newOrg(t, f.userID, "beta")
	f.h.saveOrg(t, beta, f.userID)
	other, err := project.New(beta.ID, "own", "Own")
	require.NoError(t, err)
	require.NoError(t, f.h.projs.Save(context.Background(), other))
	key := f.key(allScopes())

	req := connect.NewRequest(&yasakuv1.UpdateSettingsRequest{Target: target("beta", "own"), Timezone: proto.String("Asia/Jakarta")})
	withKey(key)(req.Header())
	_, err = f.h.ledgerClient().UpdateSettings(t.Context(), req)
	require.Equal(t, connect.CodeNotFound, connectCode(err), "err=%v", err)
	require.Zero(t, f.h.ledger.Len())

	list := connect.NewRequest(&yasakuv1.ListProjectsRequest{})
	withKey(key)(list.Header())
	resp, err := f.h.workspaceClient().ListProjects(t.Context(), list)
	require.NoError(t, err)
	require.Len(t, resp.Msg.GetProjects(), 1)
	require.Equal(t, "acme", resp.Msg.GetProjects()[0].GetOrg())
}

func TestKeyStillReachesItsOwnProject(t *testing.T) {
	f := newReachFixture(t)
	key := f.key(allScopes())

	for _, tg := range []*yasakuv1.Target{target("", "own"), target("acme", "Own"), {}} {
		req := connect.NewRequest(&yasakuv1.GetSettingsRequest{Target: tg})
		withKey(key)(req.Header())
		_, err := f.h.ledgerClient().GetSettings(t.Context(), req)
		require.NoError(t, err, "target %v must resolve to the key's own project", tg)
	}

	upd := connect.NewRequest(&yasakuv1.UpdateSettingsRequest{Timezone: proto.String("Asia/Jakarta")})
	withKey(key)(upd.Header())
	_, err := f.h.ledgerClient().UpdateSettings(t.Context(), upd)
	require.NoError(t, err)
	require.Equal(t, 1, f.h.ledger.Len())
}

// SECURITY: project_list hands an agent the ids every other tool takes, so a key must see only the projects it reaches.
func TestKeyProjectListShowsOnlyReachableProjects(t *testing.T) {
	f := newReachFixture(t)
	key := f.key(allScopes())

	req := connect.NewRequest(&projectv1.ListProjectsRequest{})
	withKey(key)(req.Header())
	resp, err := f.h.projectClient().ListProjects(t.Context(), req)
	require.NoError(t, err)

	ids := make([]string, 0, len(resp.Msg.GetProjects()))
	for _, p := range resp.Msg.GetProjects() {
		ids = append(ids, p.GetId())
	}
	require.Equal(t, []string{f.own.ID.String()}, ids)

	wreq := connect.NewRequest(&yasakuv1.ListProjectsRequest{})
	withKey(key)(wreq.Header())
	wresp, err := f.h.workspaceClient().ListProjects(t.Context(), wreq)
	require.NoError(t, err)
	require.Len(t, wresp.Msg.GetProjects(), 1)
	require.Equal(t, "own", wresp.Msg.GetProjects()[0].GetProject())
}

func TestPersonStillReachesEverySiblingProject(t *testing.T) {
	f := newReachFixture(t)

	getReq := connect.NewRequest(&yasakuv1.GetSettingsRequest{Target: target("", "sibling")})
	withBearer(getReq.Header())
	_, err := f.h.ledgerClient().GetSettings(t.Context(), getReq)
	require.NoError(t, err, "org membership reaches every project in the org")

	projReq := connect.NewRequest(&projectv1.ListProjectsRequest{})
	withBearer(projReq.Header())
	resp, err := f.h.projectClient().ListProjects(t.Context(), projReq)
	require.NoError(t, err)
	require.Len(t, resp.Msg.GetProjects(), 2)

	wreq := connect.NewRequest(&yasakuv1.ListProjectsRequest{})
	withBearer(wreq.Header())
	wresp, err := f.h.workspaceClient().ListProjects(t.Context(), wreq)
	require.NoError(t, err)
	require.Len(t, wresp.Msg.GetProjects(), 2)
}

// SECURITY: a key restricted to named resources never reaches a project-wide yasaku verb.
func TestResourceBoundKeyReachesNoWholeProject(t *testing.T) {
	f := newReachFixture(t)
	key := f.key(allScopes(), uuid.New())

	req := connect.NewRequest(&yasakuv1.UpdateSettingsRequest{Target: target("", "own"), Timezone: proto.String("Asia/Jakarta")})
	withKey(key)(req.Header())
	_, err := f.h.ledgerClient().UpdateSettings(t.Context(), req)
	require.Equal(t, connect.CodeNotFound, connectCode(err), "err=%v", err)
	require.Zero(t, f.h.ledger.Len())
}

func withKeyReq[T any](key string, msg *T) *connect.Request[T] {
	req := connect.NewRequest(msg)
	withKey(key)(req.Header())
	return req
}

// TestResourceBoundKeyReachesOnlyItsResources is yasaku's form of the template guard: every yasaku verb addresses a whole project, so a key bound to named resources reaches none of them, in its own project or any other. SECURITY: revert ReachesWholeProject in scopeToTarget and the own-project rows fail.
func TestResourceBoundKeyReachesOnlyItsResources(t *testing.T) {
	f := newReachFixture(t)
	for name, bound := range map[string]uuid.UUID{"an unrelated resource": uuid.New(), "its own project id": f.own.ID} {
		t.Run(name, func(t *testing.T) {
			key := f.key(allScopes(), bound)
			_, err := f.h.ledgerClient().GetSettings(t.Context(), withKeyReq(key, &yasakuv1.GetSettingsRequest{}))
			require.Equal(t, connect.CodeInvalidArgument, connectCode(err),
				"with no target the key must see no reachable project at all; err=%v", err)
			for _, tg := range []*yasakuv1.Target{target("", "own"), target("acme", "Own"), target("", "sibling")} {
				get := connect.NewRequest(&yasakuv1.GetSettingsRequest{Target: tg})
				withKey(key)(get.Header())
				_, err = f.h.ledgerClient().GetSettings(t.Context(), get)
				require.Equal(t, connect.CodeNotFound, connectCode(err), "read %v: err=%v", tg, err)

				upd := connect.NewRequest(&yasakuv1.UpdateSettingsRequest{Target: tg, Timezone: proto.String("Asia/Jakarta")})
				withKey(key)(upd.Header())
				_, err = f.h.ledgerClient().UpdateSettings(t.Context(), upd)
				require.Equal(t, connect.CodeNotFound, connectCode(err), "write %v: err=%v", tg, err)

				now := connect.NewRequest(&yasakuv1.NowRequest{Target: tg})
				withKey(key)(now.Header())
				_, err = f.h.workspaceClient().Now(t.Context(), now)
				require.Equal(t, connect.CodeNotFound, connectCode(err), "now %v: err=%v", tg, err)
			}
			require.Zero(t, f.h.ledger.Len(), "a resource-bound key wrote project settings")
		})
	}

	whole := f.key(allScopes())
	get := connect.NewRequest(&yasakuv1.GetSettingsRequest{Target: target("", "own")})
	withKey(whole)(get.Header())
	_, err := f.h.ledgerClient().GetSettings(t.Context(), get)
	require.NoError(t, err, "an unrestricted key must still reach its project, or the refusals above prove nothing")
}

func TestReadKeyCannotWrite(t *testing.T) {
	f := newReachFixture(t)
	key := f.key([]string{authn.ScopeYasakuRead})

	get := connect.NewRequest(&yasakuv1.GetSettingsRequest{})
	withKey(key)(get.Header())
	_, err := f.h.ledgerClient().GetSettings(t.Context(), get)
	require.NoError(t, err)

	upd := connect.NewRequest(&yasakuv1.UpdateSettingsRequest{Timezone: proto.String("Asia/Jakarta")})
	withKey(key)(upd.Header())
	_, err = f.h.ledgerClient().UpdateSettings(t.Context(), upd)
	require.Equal(t, connect.CodePermissionDenied, connectCode(err), "err=%v", err)
	require.Zero(t, f.h.ledger.Len())
}
