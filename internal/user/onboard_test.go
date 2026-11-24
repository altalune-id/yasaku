package user_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/limits"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/user"
)

type fakeOrgs struct {
	mu             sync.Mutex
	byID           map[uuid.UUID]*user.OrgRef
	memberships    map[[2]uuid.UUID]*user.MembershipRef
	userToOrgs     map[uuid.UUID][]uuid.UUID
	ListForUserErr error
	// SaveMembershipErr makes every SaveMembership call fail with it.
	SaveMembershipErr error
}

func newFakeOrgs() *fakeOrgs {
	return &fakeOrgs{
		byID:        map[uuid.UUID]*user.OrgRef{},
		memberships: map[[2]uuid.UUID]*user.MembershipRef{},
		userToOrgs:  map[uuid.UUID][]uuid.UUID{},
	}
}

func (f *fakeOrgs) SystemOrg(_ context.Context) (*user.OrgRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var found *user.OrgRef
	for _, o := range f.byID {
		if o.System && (found == nil || o.CreatedAt.Before(found.CreatedAt)) {
			found = o
		}
	}
	if found == nil {
		return nil, &user.SingletonOrgMissingError{}
	}
	cp := *found
	return &cp, nil
}

func (f *fakeOrgs) Save(_ context.Context, o *user.OrgRef) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *o
	f.byID[o.ID] = &cp
	return nil
}

func (f *fakeOrgs) ListForUser(_ context.Context, userID uuid.UUID) ([]*user.OrgRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ListForUserErr != nil {
		return nil, f.ListForUserErr
	}
	ids := f.userToOrgs[userID]
	out := make([]*user.OrgRef, 0, len(ids))
	for _, id := range ids {
		if o, ok := f.byID[id]; ok {
			cp := *o
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (f *fakeOrgs) MembershipOf(_ context.Context, orgID, userID uuid.UUID) (*user.MembershipRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if m, ok := f.memberships[[2]uuid.UUID{orgID, userID}]; ok {
		cp := *m
		return &cp, nil
	}
	return nil, &user.NotFoundError{}
}

func (f *fakeOrgs) SaveMembership(_ context.Context, m *user.MembershipRef) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.SaveMembershipErr != nil {
		return f.SaveMembershipErr
	}
	cp := *m
	f.memberships[[2]uuid.UUID{m.OrgID, m.UserID}] = &cp
	f.userToOrgs[m.UserID] = append(f.userToOrgs[m.UserID], m.OrgID)
	return nil
}

type fakeProjects struct {
	mu     sync.Mutex
	byOrg  map[uuid.UUID][]*user.ProjectRef
	bySlug map[string]*user.ProjectRef
}

func newFakeProjects() *fakeProjects {
	return &fakeProjects{
		byOrg:  map[uuid.UUID][]*user.ProjectRef{},
		bySlug: map[string]*user.ProjectRef{},
	}
}

func (f *fakeProjects) BySlug(_ context.Context, orgID uuid.UUID, slug string) (*user.ProjectRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.bySlug[orgID.String()+"/"+slug]; ok {
		cp := *p
		return &cp, nil
	}
	return nil, &user.NotFoundError{}
}

func (f *fakeProjects) Save(_ context.Context, p *user.ProjectRef) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *p
	f.byOrg[p.OrgID] = append(f.byOrg[p.OrgID], &cp)
	f.bySlug[p.OrgID.String()+"/"+p.Slug] = &cp
	return nil
}

func (f *fakeProjects) ListByOrg(_ context.Context, orgID uuid.UUID) ([]*user.ProjectRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*user.ProjectRef, 0, len(f.byOrg[orgID]))
	for _, p := range f.byOrg[orgID] {
		cp := *p
		out = append(out, &cp)
	}
	return out, nil
}

type fakeInvites struct {
	mu    sync.Mutex
	byOrg map[uuid.UUID][]*user.InviteRef
	byID  map[uuid.UUID]*user.InviteRef
}

func newFakeInvites() *fakeInvites {
	return &fakeInvites{
		byOrg: map[uuid.UUID][]*user.InviteRef{},
		byID:  map[uuid.UUID]*user.InviteRef{},
	}
}

func (f *fakeInvites) ListByOrg(_ context.Context, orgID uuid.UUID) ([]*user.InviteRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*user.InviteRef, 0, len(f.byOrg[orgID]))
	for _, inv := range f.byOrg[orgID] {
		if stored, ok := f.byID[inv.ID]; ok {
			cp := *stored
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (f *fakeInvites) ListPendingForEmail(_ context.Context, email string) ([]*user.InviteRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*user.InviteRef, 0)
	for _, inv := range f.byID {
		if inv.AcceptedAt != nil {
			continue
		}
		if !strings.EqualFold(inv.Email, email) {
			continue
		}
		cp := *inv
		out = append(out, &cp)
	}
	return out, nil
}

func (f *fakeInvites) Save(ctx context.Context, inv *user.InviteRef) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *inv
	apply := func() {
		if _, ok := f.byID[inv.ID]; !ok {
			f.byOrg[inv.OrgID] = append(f.byOrg[inv.OrgID], inv)
		}
		f.byID[inv.ID] = &cp
	}
	if _, inTx := db.CurrentTx(ctx); inTx {
		db.AfterCommit(ctx, func() {
			f.mu.Lock()
			defer f.mu.Unlock()
			apply()
		})
		return nil
	}
	apply()
	return nil
}

func (f *fakeInvites) pending(id uuid.UUID) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	inv, ok := f.byID[id]
	return ok && inv.AcceptedAt == nil
}

func TestOnboard_Selfhosted_AcceptsInvite(t *testing.T) {
	t.Parallel()
	orgs := newFakeOrgs()
	projects := newFakeProjects()
	invites := newFakeInvites()
	users := fakes.NewUser()

	singleton := &user.OrgRef{ID: uuid.New(), Slug: "primary", Name: "Primary", OwnerID: uuid.New(), CreatedAt: time.Now().UTC(), System: true}
	if err := orgs.Save(context.Background(), singleton); err != nil {
		t.Fatal(err)
	}

	inviteID := uuid.New()
	if err := invites.Save(context.Background(), &user.InviteRef{
		ID: inviteID, OrgID: singleton.ID, Email: "alice@example.com", Role: "member",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	policy := user.Policy{Mode: user.PolicyModeSelfhosted}
	wf := user.NewOnboardWorkflow(users, orgs, projects, invites, policy, newTestLogger(), noopUnexpected())

	userID := uuid.New()
	res, err := wf.Onboard(context.Background(), userID, "alice@example.com")
	if err != nil {
		t.Fatalf("Onboard: %v", err)
	}
	if res.OrgID != singleton.ID {
		t.Errorf("orgID=%v want %v", res.OrgID, singleton.ID)
	}
	m, err := orgs.MembershipOf(context.Background(), singleton.ID, userID)
	if err != nil {
		t.Fatalf("expected membership, got err %v", err)
	}
	if m.Role != "member" {
		t.Errorf("role=%q want member", m.Role)
	}
	stored := invites.byID[inviteID]
	if stored.AcceptedAt == nil {
		t.Error("invite must be marked accepted")
	}
}

func TestOnboard_Selfhosted_FindsTheSystemOrgWhateverItsSlug(t *testing.T) {
	t.Parallel()
	orgs := newFakeOrgs()
	plain := &user.OrgRef{ID: uuid.New(), Slug: "default", Name: "Plain", OwnerID: uuid.New(), CreatedAt: time.Now().UTC().Add(-time.Hour)}
	singleton := &user.OrgRef{ID: uuid.New(), Slug: "wispy-frost-4821", Name: "Edited", OwnerID: uuid.New(), CreatedAt: time.Now().UTC(), System: true}
	for _, o := range []*user.OrgRef{plain, singleton} {
		if err := orgs.Save(context.Background(), o); err != nil {
			t.Fatal(err)
		}
	}
	adminID := uuid.New()
	if err := orgs.SaveMembership(context.Background(), &user.MembershipRef{OrgID: singleton.ID, UserID: adminID, Role: "owner"}); err != nil {
		t.Fatal(err)
	}

	wf := user.NewOnboardWorkflow(fakes.NewUser(), orgs, newFakeProjects(), newFakeInvites(),
		user.Policy{Mode: user.PolicyModeSelfhosted}, newTestLogger(), noopUnexpected())

	res, err := wf.Onboard(context.Background(), adminID, "admin@example.com")
	if err != nil {
		t.Fatalf("Onboard: %v", err)
	}
	if res.OrgID != singleton.ID {
		t.Fatalf("orgID=%v, want the system org %v", res.OrgID, singleton.ID)
	}

	_, err = wf.Onboard(context.Background(), uuid.New(), "stranger@example.com")
	if !user.IsNotInvitedError(err) {
		t.Fatalf("an uninvited user must see NotInvitedError once the system org exists, got %T: %v", err, err)
	}
}

func TestOnboard_Selfhosted_NoSystemOrgIsSignupRequired(t *testing.T) {
	t.Parallel()
	orgs := newFakeOrgs()
	plain := &user.OrgRef{ID: uuid.New(), Slug: "default", Name: "Plain", OwnerID: uuid.New(), CreatedAt: time.Now().UTC()}
	if err := orgs.Save(context.Background(), plain); err != nil {
		t.Fatal(err)
	}

	wf := user.NewOnboardWorkflow(fakes.NewUser(), orgs, newFakeProjects(), newFakeInvites(),
		user.Policy{Mode: user.PolicyModeSelfhosted}, newTestLogger(), noopUnexpected())

	_, err := wf.Onboard(context.Background(), uuid.New(), "alice@example.com")
	if !user.IsSignupRequiredError(err) {
		t.Fatalf("a non-system org must not stand in for the singleton, got %T: %v", err, err)
	}
}

func TestOnboard_Selfhosted_NotInvited(t *testing.T) {
	t.Parallel()
	orgs := newFakeOrgs()
	singleton := &user.OrgRef{ID: uuid.New(), Slug: "primary", Name: "Primary", OwnerID: uuid.New(), CreatedAt: time.Now().UTC(), System: true}
	if err := orgs.Save(context.Background(), singleton); err != nil {
		t.Fatal(err)
	}

	policy := user.Policy{Mode: user.PolicyModeSelfhosted}
	wf := user.NewOnboardWorkflow(fakes.NewUser(), orgs, newFakeProjects(), newFakeInvites(), policy, newTestLogger(), noopUnexpected())

	_, err := wf.Onboard(context.Background(), uuid.New(), "alice@example.com")
	if err == nil {
		t.Fatal("expected NotInvitedError")
	}
	if !user.IsNotInvitedError(err) {
		t.Errorf("want IsNotInvitedError, got %T: %v", err, err)
	}
}

func TestOnboard_Cloud_NoMembershipNoInviteReturnsSignupRequired(t *testing.T) {
	t.Parallel()
	orgs := newFakeOrgs()
	projects := newFakeProjects()
	invites := newFakeInvites()
	users := fakes.NewUser()

	policy := user.Policy{Mode: user.PolicyModeCloud}
	wf := user.NewOnboardWorkflow(users, orgs, projects, invites, policy, newTestLogger(), noopUnexpected())

	_, err := wf.Onboard(context.Background(), uuid.New(), "alice@example.com")
	if err == nil {
		t.Fatal("expected SignupRequiredError")
	}
	if !user.IsSignupRequiredError(err) {
		t.Errorf("want IsSignupRequiredError, got %T: %v", err, err)
	}
	if len(orgs.byID) != 0 {
		t.Errorf("no org must be created; got %d", len(orgs.byID))
	}
}

func TestOnboard_Cloud_AcceptsInvite(t *testing.T) {
	t.Parallel()
	orgs := newFakeOrgs()
	projects := newFakeProjects()
	invites := newFakeInvites()
	users := fakes.NewUser()

	orgA := &user.OrgRef{ID: uuid.New(), Slug: "acme", Name: "Acme", OwnerID: uuid.New(), CreatedAt: time.Now().UTC()}
	if err := orgs.Save(context.Background(), orgA); err != nil {
		t.Fatal(err)
	}
	inviteID := uuid.New()
	if err := invites.Save(context.Background(), &user.InviteRef{
		ID: inviteID, OrgID: orgA.ID, Email: "alice@example.com", Role: "admin",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	policy := user.Policy{Mode: user.PolicyModeCloud}
	wf := user.NewOnboardWorkflow(users, orgs, projects, invites, policy, newTestLogger(), noopUnexpected())

	userID := uuid.New()
	res, err := wf.Onboard(context.Background(), userID, "alice@example.com")
	if err != nil {
		t.Fatalf("Onboard: %v", err)
	}
	if res.OrgID != orgA.ID {
		t.Errorf("orgID=%v want %v", res.OrgID, orgA.ID)
	}
	m, err := orgs.MembershipOf(context.Background(), orgA.ID, userID)
	if err != nil {
		t.Fatalf("expected membership, got err %v", err)
	}
	if m.Role != "admin" {
		t.Errorf("role=%q want admin", m.Role)
	}
	if invites.byID[inviteID].AcceptedAt == nil {
		t.Error("invite must be marked accepted")
	}
}

func TestOnboard_UnknownMode(t *testing.T) {
	t.Parallel()
	wf := user.NewOnboardWorkflow(
		fakes.NewUser(),
		newFakeOrgs(),
		newFakeProjects(),
		newFakeInvites(),
		user.Policy{Mode: "invalid"},
		newTestLogger(),
		noopUnexpected(),
	)
	_, err := wf.Onboard(context.Background(), uuid.New(), "a@b.co")
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}
}

func seedCloudInvite(t *testing.T, invites *fakeInvites, orgs *fakeOrgs, email string, created time.Time) (orgID, inviteID uuid.UUID) { //nolint:nonamedreturns // two ids of one type read cleaner named
	t.Helper()
	orgID, inviteID = uuid.New(), uuid.New()
	if err := orgs.Save(context.Background(), &user.OrgRef{ID: orgID, Slug: orgID.String()[:8], Name: "Org", OwnerID: uuid.New(), CreatedAt: created}); err != nil {
		t.Fatal(err)
	}
	if err := invites.Save(context.Background(), &user.InviteRef{
		ID: inviteID, OrgID: orgID, Email: email, Role: "member", ExpiresAt: time.Now().UTC().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	return orgID, inviteID
}

func TestOnboardCloud_AcceptIsAtomic(t *testing.T) {
	t.Parallel()
	orgs := newFakeOrgs()
	invites := newFakeInvites()
	_, inviteID := seedCloudInvite(t, invites, orgs, "alice@example.com", time.Now().UTC())
	orgs.SaveMembershipErr = errors.New("membership insert failed")

	wf := user.NewOnboardWorkflow(fakes.NewUser(), orgs, newFakeProjects(), invites, user.Policy{Mode: user.PolicyModeCloud},
		newTestLogger(), noopUnexpected(), user.WithOnboardUnitOfWork(fakes.UnitOfWork))
	if _, err := wf.Onboard(context.Background(), uuid.New(), "alice@example.com"); err == nil {
		t.Fatal("a failed membership save must fail onboarding")
	}
	if !invites.pending(inviteID) {
		t.Fatal("the invite must stay pending when its membership save rolls back")
	}
}

func TestOnboardCloud_PendingInviteBecomesFirstOrg(t *testing.T) {
	t.Parallel()
	orgs := newFakeOrgs()
	invites := newFakeInvites()
	orgID, inviteID := seedCloudInvite(t, invites, orgs, "alice@example.com", time.Now().UTC())

	wf := user.NewOnboardWorkflow(fakes.NewUser(), orgs, newFakeProjects(), invites, user.Policy{Mode: user.PolicyModeCloud},
		newTestLogger(), noopUnexpected(), user.WithOnboardUnitOfWork(fakes.UnitOfWork))
	userID := uuid.New()
	res, err := wf.Onboard(context.Background(), userID, "alice@example.com")
	if err != nil {
		t.Fatalf("Onboard: %v", err)
	}
	if res.OrgID != orgID {
		t.Fatalf("OrgID = %v, want the inviting org %v", res.OrgID, orgID)
	}
	if invites.pending(inviteID) {
		t.Fatal("the invite must be accepted at sign-in")
	}
	if _, err := orgs.MembershipOf(context.Background(), orgID, userID); err != nil {
		t.Fatalf("membership missing: %v", err)
	}
}

func TestOnboardCloud_SecondInviteStaysAcceptable(t *testing.T) {
	t.Parallel()
	orgs := newFakeOrgs()
	invites := newFakeInvites()
	_, first := seedCloudInvite(t, invites, orgs, "alice@example.com", time.Now().UTC())
	_, second := seedCloudInvite(t, invites, orgs, "alice@example.com", time.Now().UTC())

	wf := user.NewOnboardWorkflow(fakes.NewUser(), orgs, newFakeProjects(), invites, user.Policy{Mode: user.PolicyModeCloud},
		newTestLogger(), noopUnexpected(), user.WithOnboardUnitOfWork(fakes.UnitOfWork))
	if _, err := wf.Onboard(context.Background(), uuid.New(), "alice@example.com"); err != nil {
		t.Fatalf("Onboard: %v", err)
	}
	if invites.pending(first) == invites.pending(second) {
		t.Fatalf("exactly one invite is accepted at sign-in: first pending=%v, second pending=%v", invites.pending(first), invites.pending(second))
	}
}

type fullOrgCounter struct{}

func (fullOrgCounter) CountMembers(context.Context, uuid.UUID) (int64, error)    { return 5, nil }
func (fullOrgCounter) CountOrgsJoined(context.Context, uuid.UUID) (int64, error) { return 0, nil }
func (fullOrgCounter) OrgName(context.Context, uuid.UUID) (string, error)        { return "Acme", nil }
func (fullOrgCounter) IsStaff(context.Context, uuid.UUID) (bool, error)          { return false, nil }

func TestOnboardCloud_AutoAcceptAtLimitStillSignsIn(t *testing.T) {
	t.Parallel()
	orgs := newFakeOrgs()
	invites := newFakeInvites()
	_, inviteID := seedCloudInvite(t, invites, orgs, "alice@example.com", time.Now().UTC())

	wf := user.NewOnboardWorkflow(fakes.NewUser(), orgs, newFakeProjects(), invites, user.Policy{Mode: user.PolicyModeCloud},
		newTestLogger(), noopUnexpected(), user.WithOnboardUnitOfWork(fakes.UnitOfWork),
		user.WithOnboardLimits(limits.NewService(fakes.NewLimits(), config.ModeCloud), fullOrgCounter{}))
	_, err := wf.Onboard(context.Background(), uuid.New(), "alice@example.com")
	sr, ok := errors.AsType[*user.SignupRequiredError](err)
	if !ok {
		t.Fatalf("a full org must not fail sign-in; want *SignupRequiredError, got %T: %v", err, err)
	}
	re, ok := errors.AsType[*limits.ReachedError](sr.JoinRefused)
	if !ok || re.Key != limits.OrgMembers || re.Name != "Acme" {
		t.Fatalf("JoinRefused must carry the org.members refusal for Acme, got %v", sr.JoinRefused)
	}
	if !invites.pending(inviteID) {
		t.Fatal("the invite stays pending, acceptable later by link")
	}
}

type joinedCounter struct {
	joined int64
	staff  bool
}

func (joinedCounter) CountMembers(context.Context, uuid.UUID) (int64, error) { return 0, nil }
func (c joinedCounter) CountOrgsJoined(context.Context, uuid.UUID) (int64, error) {
	return c.joined, nil
}
func (joinedCounter) OrgName(context.Context, uuid.UUID) (string, error) { return "Acme", nil }
func (c joinedCounter) IsStaff(context.Context, uuid.UUID) (bool, error) { return c.staff, nil }

func TestOnboardCloud_StaffExemptFromOrgsJoined(t *testing.T) {
	t.Parallel()
	for name, staff := range map[string]bool{"staff joins past the limit": true, "a user at the limit is refused": false} {
		t.Run(name, func(t *testing.T) {
			orgs := newFakeOrgs()
			invites := newFakeInvites()
			_, inviteID := seedCloudInvite(t, invites, orgs, "alice@example.com", time.Now().UTC())
			wf := user.NewOnboardWorkflow(fakes.NewUser(), orgs, newFakeProjects(), invites, user.Policy{Mode: user.PolicyModeCloud},
				newTestLogger(), noopUnexpected(), user.WithOnboardUnitOfWork(fakes.UnitOfWork),
				user.WithOnboardLimits(limits.NewService(fakes.NewLimits(), config.ModeCloud), joinedCounter{joined: 5, staff: staff}))
			_, err := wf.Onboard(context.Background(), uuid.New(), "alice@example.com")
			if staff {
				if err != nil || invites.pending(inviteID) {
					t.Fatalf("staff skip user.orgs_joined, as invite accept does: err=%v pending=%v", err, invites.pending(inviteID))
				}
				return
			}
			if !invites.pending(inviteID) {
				t.Fatal("a non-staff user at five orgs keeps the invite pending")
			}
		})
	}
}

func TestOnboardSelfhosted_AutoAcceptAtLimitStillSignsIn(t *testing.T) {
	t.Parallel()
	orgs := newFakeOrgs()
	singleton := &user.OrgRef{ID: uuid.New(), Slug: "home", Name: "Acme", OwnerID: uuid.New(), CreatedAt: time.Now().UTC(), System: true}
	if err := orgs.Save(context.Background(), singleton); err != nil {
		t.Fatal(err)
	}
	invites := newFakeInvites()
	inviteID := uuid.New()
	if err := invites.Save(context.Background(), &user.InviteRef{
		ID: inviteID, OrgID: singleton.ID, Email: "alice@example.com", Role: "member",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	lstore := fakes.NewLimits()
	if err := lstore.SetOverride(context.Background(), limits.Subject{Kind: limits.SubjectOrg, ID: singleton.ID}, limits.OrgMembers, 0, uuid.Nil); err != nil {
		t.Fatal(err)
	}
	wf := user.NewOnboardWorkflow(fakes.NewUser(), orgs, newFakeProjects(), invites, user.Policy{Mode: user.PolicyModeSelfhosted},
		newTestLogger(), noopUnexpected(), user.WithOnboardUnitOfWork(fakes.UnitOfWork),
		user.WithOnboardLimits(limits.NewService(lstore, config.ModeSelfhosted), fullOrgCounter{}))

	_, err := wf.Onboard(context.Background(), uuid.New(), "alice@example.com")
	sr, ok := errors.AsType[*user.SignupRequiredError](err)
	if !ok {
		t.Fatalf("a full org must not fail sign-in; want *SignupRequiredError, got %T: %v", err, err)
	}
	if re, ok := errors.AsType[*limits.ReachedError](sr.JoinRefused); !ok || re.Key != limits.OrgMembers {
		t.Fatalf("JoinRefused must carry the org.members refusal, got %v", sr.JoinRefused)
	}
	if !invites.pending(inviteID) {
		t.Fatal("the invite stays pending, acceptable once a seat frees")
	}
}

func TestOnboardMemberJoined_AuditedInSameTx(t *testing.T) {
	t.Parallel()
	orgs := newFakeOrgs()
	invites := newFakeInvites()
	orgID, _ := seedCloudInvite(t, invites, orgs, "alice@example.com", time.Now().UTC())
	a := &fakes.Audit{}
	wf := user.NewOnboardWorkflow(fakes.NewUser(), orgs, newFakeProjects(), invites, user.Policy{Mode: user.PolicyModeCloud},
		newTestLogger(), noopUnexpected(), user.WithOnboardUnitOfWork(fakes.UnitOfWork), user.WithOnboardAudit(a))
	userID := uuid.New()
	if _, err := wf.Onboard(context.Background(), userID, "alice@example.com"); err != nil {
		t.Fatal(err)
	}
	recs := a.Records()
	if len(recs) != 1 || recs[0].Action != "member.joined" || recs[0].OrgID != orgID || recs[0].TargetID != userID.String() {
		t.Fatalf("got %+v", recs)
	}
}
