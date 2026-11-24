package invite_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/invite"
	"altalune.id/yasaku/internal/limits"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/fakes"
)

type fakeCounter struct {
	members int64
	joined  int64
	staff   bool
	name    string
}

func (c fakeCounter) CountMembers(context.Context, uuid.UUID) (int64, error)    { return c.members, nil }
func (c fakeCounter) CountOrgsJoined(context.Context, uuid.UUID) (int64, error) { return c.joined, nil }
func (c fakeCounter) IsStaff(context.Context, uuid.UUID) (bool, error)          { return c.staff, nil }
func (c fakeCounter) OrgName(context.Context, uuid.UUID) (string, error)        { return c.name, nil }

func cloudLimits() *limits.Service { return limits.NewService(fakes.NewLimits(), config.ModeCloud) }

func TestInviteSend_SeatsCountPending(t *testing.T) {
	t.Parallel()
	store := fakes.NewInvite()
	unex := 0
	wf := invite.NewSendWorkflow(store, &recordingMailer{}, "https://yasaku.example.test", newTestLogger(), noopUnexpected(&unex),
		invite.WithSendUnitOfWork(fakes.UnitOfWork), invite.WithSendLimits(cloudLimits(), fakeCounter{members: 3, name: "Acme"}))
	ctx := fakes.Owner(context.Background(), tenant.Context{OrgID: uuid.New(), UserID: uuid.New()})

	if _, err := wf.Execute(ctx, invite.SendRequest{Email: "a@example.com", Role: invite.RoleMember}); err != nil {
		t.Fatalf("fourth seat: %v", err)
	}
	if _, err := wf.Execute(ctx, invite.SendRequest{Email: "b@example.com", Role: invite.RoleMember}); err != nil {
		t.Fatalf("fifth seat: %v", err)
	}
	_, err := wf.Execute(ctx, invite.SendRequest{Email: "c@example.com", Role: invite.RoleMember})
	re, ok := errors.AsType[*limits.ReachedError](err)
	if !ok {
		t.Fatalf("3 members plus 2 pending fill 5 seats, want *ReachedError, got %T: %v", err, err)
	}
	if re.Key != limits.OrgMembers || re.Pending != 2 || re.Name != "Acme" {
		t.Fatalf("got key %s pending %d name %q, want org.members with 2 pending in Acme", re.Key, re.Pending, re.Name)
	}
	if store.Len() != 2 {
		t.Fatalf("the refused invite must not be saved, have %d", store.Len())
	}
}

func TestInviteSend_ExpiredInvitesFreeSeats(t *testing.T) {
	t.Parallel()
	store := fakes.NewInvite()
	unex := 0
	now := time.Now().UTC()
	clock := now
	wf := invite.NewSendWorkflow(store, &recordingMailer{}, "https://yasaku.example.test", newTestLogger(), noopUnexpected(&unex),
		invite.WithClock(func() time.Time { return clock }),
		invite.WithSendUnitOfWork(fakes.UnitOfWork), invite.WithSendLimits(cloudLimits(), fakeCounter{members: 4}))
	ctx := fakes.Owner(context.Background(), tenant.Context{OrgID: uuid.New(), UserID: uuid.New()})
	if _, err := wf.Execute(ctx, invite.SendRequest{Email: "a@example.com", Role: invite.RoleMember, TTL: time.Hour}); err != nil {
		t.Fatal(err)
	}
	clock = now.Add(2 * time.Hour)
	if _, err := wf.Execute(ctx, invite.SendRequest{Email: "b@example.com", Role: invite.RoleMember}); err != nil {
		t.Fatalf("an expired invite holds no seat: %v", err)
	}
}

func TestAccept_SeatsCountMembersOnly(t *testing.T) {
	t.Parallel()
	store := fakes.NewInvite()
	orgs := newFakeOrgs()
	orgID := uuid.New()
	_, raw := seedAcceptable(t, store, orgs, orgID, "alice@example.com", time.Hour)
	unex := 0
	wf := invite.NewAcceptWorkflow(store, newFakeUsers(), orgs, newTestLogger(), noopUnexpected(&unex),
		invite.WithUnitOfWork(fakes.UnitOfWork), invite.WithAcceptLimits(cloudLimits(), fakeCounter{members: 4}))
	if _, err := wf.Execute(context.Background(), invite.AcceptRequest{Token: raw, Email: "alice@example.com"}); err != nil {
		t.Fatalf("the accepting invite's own pending seat must not count against it: %v", err)
	}

	_, raw2 := seedAcceptable(t, store, orgs, orgID, "bob@example.com", time.Hour)
	full := invite.NewAcceptWorkflow(store, newFakeUsers(), orgs, newTestLogger(), noopUnexpected(&unex),
		invite.WithUnitOfWork(fakes.UnitOfWork), invite.WithAcceptLimits(cloudLimits(), fakeCounter{members: 5}))
	_, err := full.Execute(context.Background(), invite.AcceptRequest{Token: raw2, Email: "bob@example.com"})
	re, ok := errors.AsType[*limits.ReachedError](err)
	if !ok || re.Key != limits.OrgMembers || re.Name != "Acme" {
		t.Fatalf("five members fill the org, want org.members naming Acme, got %T: %v", err, err)
	}
}

func TestAccept_SixthOrgRefused(t *testing.T) {
	t.Parallel()
	store := fakes.NewInvite()
	orgs := newFakeOrgs()
	orgID := uuid.New()
	_, raw := seedAcceptable(t, store, orgs, orgID, "alice@example.com", time.Hour)
	unex := 0
	wf := invite.NewAcceptWorkflow(store, newFakeUsers(), orgs, newTestLogger(), noopUnexpected(&unex),
		invite.WithUnitOfWork(fakes.UnitOfWork), invite.WithAcceptLimits(cloudLimits(), fakeCounter{joined: 5}))
	_, err := wf.Execute(context.Background(), invite.AcceptRequest{Token: raw, Email: "alice@example.com"})
	re, ok := errors.AsType[*limits.ReachedError](err)
	if !ok || re.Key != limits.UserOrgsJoined {
		t.Fatalf("want user.orgs_joined, got %T: %v", err, err)
	}
	if pending, _ := store.ListPending(context.Background(), orgID); len(pending) != 1 {
		t.Fatal("a refused accept leaves the invite pending")
	}
}

func TestSeats_CountsMembersAndPending(t *testing.T) {
	t.Parallel()
	store := fakes.NewInvite()
	unex := 0
	send := invite.NewSendWorkflow(store, &recordingMailer{}, "https://yasaku.example.test", newTestLogger(), noopUnexpected(&unex),
		invite.WithSendUnitOfWork(fakes.UnitOfWork), invite.WithSendLimits(cloudLimits(), fakeCounter{members: 2}))
	svc := invite.NewService(store, send, nil, true, newTestLogger(), noopUnexpected(&unex))
	ctx := fakes.Owner(context.Background(), tenant.Context{OrgID: uuid.New(), UserID: uuid.New()})
	if _, err := svc.Send(ctx, invite.SendRequest{Email: "a@example.com", Role: invite.RoleMember}); err != nil {
		t.Fatal(err)
	}
	seats, err := svc.Seats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if seats != (invite.Seats{Used: 3, Max: 5, Pending: 1}) || seats.Unlimited() {
		t.Fatalf("got %+v, want 3 of 5 with 1 pending", seats)
	}

	plain := invite.NewService(store, invite.NewSendWorkflow(store, &recordingMailer{}, "", newTestLogger(), noopUnexpected(&unex)), nil, true, newTestLogger(), noopUnexpected(&unex))
	if seats, err := plain.Seats(ctx); err != nil || !seats.Unlimited() {
		t.Fatalf("no limiter reads as unlimited, got %+v %v", seats, err)
	}
}

func TestInviteSent_AuditedInSameTx(t *testing.T) {
	t.Parallel()
	store := fakes.NewInvite()
	a := &fakes.Audit{}
	unex := 0
	wf := invite.NewSendWorkflow(store, &recordingMailer{}, "https://yasaku.example.test", newTestLogger(), noopUnexpected(&unex),
		invite.WithSendUnitOfWork(fakes.UnitOfWork), invite.WithSendAudit(a))
	orgID := uuid.New()
	inv, err := wf.Execute(fakes.Owner(context.Background(), tenant.Context{OrgID: orgID, UserID: uuid.New()}),
		invite.SendRequest{Email: "a@example.com", Role: invite.RoleMember})
	if err != nil {
		t.Fatal(err)
	}
	recs := a.Records()
	if len(recs) != 1 || recs[0].Action != "invite.sent" || recs[0].TargetID != inv.ID.String() || recs[0].OrgID != orgID || recs[0].Detail["name"] != "a@example.com" {
		t.Fatalf("got %+v", recs)
	}
}

func TestInviteRevoked_AuditedInSameTx(t *testing.T) {
	t.Parallel()
	store := fakes.NewInvite()
	a := &fakes.Audit{}
	unex := 0
	send := invite.NewSendWorkflow(store, &recordingMailer{}, "", newTestLogger(), noopUnexpected(&unex))
	svc := invite.NewService(store, send, nil, true, newTestLogger(), noopUnexpected(&unex), invite.WithRevokeAudit(fakes.UnitOfWork, a))
	ctx := fakes.Owner(context.Background(), tenant.Context{OrgID: uuid.New(), UserID: uuid.New()})
	inv, err := svc.Send(ctx, invite.SendRequest{Email: "a@example.com", Role: invite.RoleMember})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Revoke(ctx, inv.ID); err != nil {
		t.Fatal(err)
	}
	if recs := a.Records(); len(recs) != 1 || recs[0].Action != "invite.revoked" || recs[0].Detail["name"] != "a@example.com" {
		t.Fatalf("got %+v", recs)
	}
}

func TestMemberJoined_AuditedInSameTx(t *testing.T) {
	t.Parallel()
	store := fakes.NewInvite()
	orgs := newFakeOrgs()
	orgID := uuid.New()
	_, raw := seedAcceptable(t, store, orgs, orgID, "alice@example.com", time.Hour)
	a := &fakes.Audit{}
	unex := 0
	wf := invite.NewAcceptWorkflow(store, newFakeUsers(), orgs, newTestLogger(), noopUnexpected(&unex),
		invite.WithUnitOfWork(fakes.UnitOfWork), invite.WithAcceptAudit(a))
	res, err := wf.Execute(context.Background(), invite.AcceptRequest{Token: raw, Email: "alice@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	recs := a.Records()
	if len(recs) != 1 || recs[0].Action != "member.joined" || recs[0].OrgID != orgID || recs[0].TargetID != res.User.ID.String() || recs[0].Detail["name"] != "alice@example.com" {
		t.Fatalf("got %+v", recs)
	}
}

func TestAccept_StaffExemptFromUserLimits(t *testing.T) {
	t.Parallel()
	store := fakes.NewInvite()
	orgs := newFakeOrgs()
	_, raw := seedAcceptable(t, store, orgs, uuid.New(), "staff@example.com", time.Hour)
	unex := 0
	wf := invite.NewAcceptWorkflow(store, newFakeUsers(), orgs, newTestLogger(), noopUnexpected(&unex),
		invite.WithUnitOfWork(fakes.UnitOfWork), invite.WithAcceptLimits(cloudLimits(), fakeCounter{joined: 5, staff: true}))
	if _, err := wf.Execute(context.Background(), invite.AcceptRequest{Token: raw, Email: "staff@example.com"}); err != nil {
		t.Fatalf("staff skip user.orgs_joined: %v", err)
	}
}
