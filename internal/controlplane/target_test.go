package controlplane

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	yasakuv1 "altalune.id/yasaku/gen/go/yasaku/v1"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/capabilities"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/testutil/fakes"
)

type targetHarness struct {
	t        *testing.T
	orgs     *org.Service
	projects *project.Service
}

func newTargetHarness(t *testing.T) *targetHarness {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	reporter := apperror.NewReporter(log, false)
	return &targetHarness{
		t:        t,
		orgs:     org.NewService(fakes.NewOrg(), capabilities.Capabilities{OrgCreation: true}, log, reporter.Unexpected),
		projects: project.NewService(fakes.NewProject(), log, reporter.Unexpected),
	}
}

func (h *targetHarness) user() uuid.UUID { return uuid.New() }

func (h *targetHarness) org(slug string) *org.Org { return h.orgFor(uuid.New(), slug) }

func (h *targetHarness) orgFor(owner uuid.UUID, slug string) *org.Org {
	h.t.Helper()
	o, err := h.orgs.Create(context.Background(), org.CreateRequest{Slug: slug, Name: slug, OwnerID: owner})
	if err != nil {
		h.t.Fatalf("create org %s: %v", slug, err)
	}
	return o
}

func (h *targetHarness) project(o *org.Org, slug string) *project.Project {
	return h.projectNamed(o, slug, slug)
}

func (h *targetHarness) projectNamed(o *org.Org, slug, name string) *project.Project {
	h.t.Helper()
	ctx := tenant.Into(context.Background(), tenant.Context{OrgID: o.ID})
	p, err := h.projects.Create(ctx, o.ID, slug, name)
	if err != nil {
		h.t.Fatalf("create project %s: %v", slug, err)
	}
	return p
}

func (h *targetHarness) keyPrincipal(o *org.Org, p *project.Project) context.Context {
	return session.PrincipalInto(context.Background(), session.Principal{
		Source:      session.SourceAPIKey,
		ActiveOrgID: o.ID,
		ProjectIDs:  []uuid.UUID{p.ID},
		KeyID:       uuid.New(),
	})
}

func (h *targetHarness) personPrincipal(u uuid.UUID) context.Context {
	return session.PrincipalInto(context.Background(), session.Principal{Source: session.SourceOIDC, UserID: u})
}

func TestScopeToTarget_ProjectKeyNeverReachesASiblingProject(t *testing.T) {
	h := newTargetHarness(t)
	o := h.org("acme")
	p1 := h.project(o, "home")
	h.project(o, "office")
	ctx := h.keyPrincipal(o, p1)

	_, _, err := scopeToTarget(ctx, h.orgs, h.projects, &yasakuv1.Target{Org: "acme", Project: "office"})
	if !IsScopeNotFoundError(err) {
		t.Fatalf("sibling project: want ScopeNotFoundError, got %v", err)
	}
}

func TestScopeToTarget_KeyIsPinnedToItsActiveOrg(t *testing.T) {
	h := newTargetHarness(t)
	a, b := h.org("acme"), h.org("beta")
	pa := h.project(a, "home")
	h.project(b, "home")
	ctx := h.keyPrincipal(a, pa)

	_, _, err := scopeToTarget(ctx, h.orgs, h.projects, &yasakuv1.Target{Org: "beta"})
	if !IsScopeNotFoundError(err) {
		t.Fatalf("other org: want ScopeNotFoundError, got %v", err)
	}
}

func TestScopeToTarget_ProjectKeyDefaultsToItsOwnProject(t *testing.T) {
	h := newTargetHarness(t)
	o := h.org("acme")
	p1 := h.project(o, "home")
	h.project(o, "office")
	ctx := h.keyPrincipal(o, p1)

	res, nd, err := scopeToTarget(ctx, h.orgs, h.projects, &yasakuv1.Target{})
	if err != nil || nd != nil {
		t.Fatalf("err=%v needs=%v", err, nd)
	}
	if res.project.ID != p1.ID {
		t.Fatalf("got project %s, want %s", res.project.Slug, p1.Slug)
	}
	tc, err := tenant.From(res.ctx)
	if err != nil || tc.OrgID != o.ID || tc.ProjectID != p1.ID {
		t.Fatalf("bound tenant = %+v err=%v, want org %s project %s", tc, err, o.ID, p1.ID)
	}
}

func TestScopeToTarget_KeyWithActiveProjectDefaultsToIt(t *testing.T) {
	h := newTargetHarness(t)
	o := h.org("acme")
	p1, p2 := h.project(o, "home"), h.project(o, "office")
	ctx := session.PrincipalInto(context.Background(), session.Principal{
		Source:          session.SourceAPIKey,
		ActiveOrgID:     o.ID,
		ActiveProjectID: p2.ID,
		AllProjects:     true,
		KeyID:           uuid.New(),
	})

	res, nd, err := scopeToTarget(ctx, h.orgs, h.projects, &yasakuv1.Target{})
	if err != nil || nd != nil {
		t.Fatalf("err=%v needs=%v", err, nd)
	}
	if res.project.ID != p2.ID {
		t.Fatalf("got project %s, want %s", res.project.Slug, p2.Slug)
	}
	_ = p1
}

func TestScopeToTarget_ResourceNarrowedKeyReachesNoWholeProject(t *testing.T) {
	h := newTargetHarness(t)
	o := h.org("acme")
	p1 := h.project(o, "home")
	ctx := session.PrincipalInto(context.Background(), session.Principal{
		Source:      session.SourceAPIKey,
		ActiveOrgID: o.ID,
		ProjectIDs:  []uuid.UUID{p1.ID},
		ResourceIDs: []uuid.UUID{uuid.New()},
		KeyID:       uuid.New(),
	})

	_, _, err := scopeToTarget(ctx, h.orgs, h.projects, &yasakuv1.Target{Project: "home"})
	if !IsScopeNotFoundError(err) {
		t.Fatalf("resource-narrowed key: want ScopeNotFoundError, got %v", err)
	}
}

func TestScopeToTarget_PersonStillChoosesAmongTheirOrgs(t *testing.T) {
	h := newTargetHarness(t)
	u := h.user()
	a, b := h.orgFor(u, "acme"), h.orgFor(u, "beta")
	h.project(a, "home")
	h.project(b, "home")
	ctx := h.personPrincipal(u)

	_, nd, err := scopeToTarget(ctx, h.orgs, h.projects, &yasakuv1.Target{})
	if err != nil || nd == nil || nd.GetNeeds()[0].GetField() != "org" {
		t.Fatalf("want needs org, got needs=%v err=%v", nd, err)
	}
}

func TestScopeToTarget_PersonNeverReachesAnOrgTheyDoNotBelongTo(t *testing.T) {
	h := newTargetHarness(t)
	u := h.user()
	h.project(h.orgFor(u, "acme"), "home")
	h.project(h.org("beta"), "home")
	ctx := h.personPrincipal(u)

	_, _, err := scopeToTarget(ctx, h.orgs, h.projects, &yasakuv1.Target{Org: "beta"})
	if !IsScopeNotFoundError(err) {
		t.Fatalf("foreign org: want ScopeNotFoundError, got %v", err)
	}
}

func TestScopeToTarget_MatchesAProjectByName(t *testing.T) {
	h := newTargetHarness(t)
	u := h.user()
	o := h.orgFor(u, "acme")
	h.projectNamed(o, "wispy-frost-4821", "Household")
	ctx := h.personPrincipal(u)

	res, nd, err := scopeToTarget(ctx, h.orgs, h.projects, &yasakuv1.Target{Org: "acme", Project: "household"})
	if err != nil || nd != nil || res.project.Name != "Household" {
		t.Fatalf("res=%v needs=%v err=%v", res.project, nd, err)
	}
}

func TestScopeToTarget_AmbiguousNameAsksForTheProject(t *testing.T) {
	h := newTargetHarness(t)
	u := h.user()
	o := h.orgFor(u, "acme")
	h.projectNamed(o, "wispy-frost-4821", "Household")
	h.projectNamed(o, "calm-river-1200", "Household")
	ctx := h.personPrincipal(u)

	_, nd, err := scopeToTarget(ctx, h.orgs, h.projects, &yasakuv1.Target{Org: "acme", Project: "household"})
	if err != nil || nd == nil || nd.GetNeeds()[0].GetField() != "project" || len(nd.GetNeeds()[0].GetCandidates()) != 2 {
		t.Fatalf("want needs project with 2 candidates, got needs=%v err=%v", nd, err)
	}
}

func TestListProjects_ProjectKeySeesOnlyItsProject(t *testing.T) {
	h := newTargetHarness(t)
	u := h.user()
	o := h.orgFor(u, "acme")
	p1 := h.project(o, "home")
	h.project(o, "office")
	h.project(h.orgFor(u, "beta"), "home")
	svc := NewWorkspaceService(h.orgs, h.projects, nil, nil)

	resp, err := svc.ListProjects(h.keyPrincipal(o, p1), connect.NewRequest(&yasakuv1.ListProjectsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	got := resp.Msg.GetProjects()
	if len(got) != 1 || got[0].GetOrg() != "acme" || got[0].GetProject() != "home" {
		t.Fatalf("project key listed %v, want only acme/home", got)
	}

	resp, err = svc.ListProjects(h.personPrincipal(u), connect.NewRequest(&yasakuv1.ListProjectsRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(resp.Msg.GetProjects()); n != 3 {
		t.Fatalf("person listed %d projects, want 3", n)
	}
}

// SECURITY: the owner belongs to both orgs, so a resolver that widened a key to its owner's orgs would reach beta; the key's ActiveOrgID must still pin it.
func TestScopeToTarget_AllProjectsOrgKeyIsPinnedToItsActiveOrg(t *testing.T) {
	h := newTargetHarness(t)
	owner := h.user()
	acme := h.orgFor(owner, "acme")
	h.project(acme, "home")
	beta, err := h.orgs.Create(context.Background(), org.CreateRequest{Slug: "beta", Name: "Beta Holdings", OwnerID: owner})
	if err != nil {
		t.Fatal(err)
	}
	h.project(beta, "home")
	ctx := session.PrincipalInto(context.Background(), session.Principal{
		Source:      session.SourceAPIKey,
		ActiveOrgID: acme.ID,
		AllProjects: true,
		KeyID:       uuid.New(),
	})

	for _, q := range []string{"beta", "Beta Holdings"} {
		_, _, err := scopeToTarget(ctx, h.orgs, h.projects, &yasakuv1.Target{Org: q, Project: "home"})
		if !IsScopeNotFoundError(err) {
			t.Errorf("org key targeting %q: want ScopeNotFoundError, got %v", q, err)
		}
	}
	res, nd, err := scopeToTarget(ctx, h.orgs, h.projects, &yasakuv1.Target{Org: "acme", Project: "home"})
	if err != nil || nd != nil || res.org.ID != acme.ID || res.principal.ActiveOrgID != acme.ID {
		t.Fatalf("own org: res=%+v needs=%v err=%v", res.org, nd, err)
	}
}
