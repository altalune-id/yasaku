package org_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/authz"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/fakes"
)

type roleOrg struct {
	svc   *org.Service
	store *fakes.Org
	org   *org.Org
	owner uuid.UUID
}

func newRoleOrg(t *testing.T) *roleOrg {
	t.Helper()
	svc, store := newTestService(t, true)
	owner := uuid.New()
	o, err := svc.Create(context.Background(), org.CreateRequest{Slug: "acme", Name: "Acme", OwnerID: owner})
	require.NoError(t, err)
	return &roleOrg{svc: svc, store: store, org: o, owner: owner}
}

func (r *roleOrg) seat(t *testing.T, role org.Role) uuid.UUID {
	t.Helper()
	id := uuid.New()
	require.NoError(t, r.store.SaveMembership(context.Background(), &org.Membership{OrgID: r.org.ID, UserID: id, Role: role}))
	return id
}

func (r *roleOrg) as(userID uuid.UUID, role org.Role) context.Context {
	return fakes.Allow(context.Background(), tenant.Context{OrgID: r.org.ID, UserID: userID}, authz.Builtin(string(role))...)
}

func (r *roleOrg) roleOf(t *testing.T, userID uuid.UUID) org.Role {
	t.Helper()
	m, err := r.store.MembershipOf(context.Background(), r.org.ID, userID)
	require.NoError(t, err)
	return m.Role
}

func TestSetRole_CoversGuard(t *testing.T) {
	r := newRoleOrg(t)
	admin, member := r.seat(t, org.RoleAdmin), r.seat(t, org.RoleMember)

	err := r.svc.SetRole(r.as(admin, org.RoleAdmin), r.org.ID, member, string(org.RoleOwner), uuid.Nil)
	assert.True(t, authz.IsEscalationError(err), "an admin cannot make an owner, got %T: %v", err, err)
	err = r.svc.SetRole(r.as(admin, org.RoleAdmin), r.org.ID, r.owner, string(org.RoleMember), uuid.Nil)
	assert.True(t, authz.IsEscalationError(err), "an admin cannot demote an owner, whose permissions it does not cover; got %T: %v", err, err)
	assert.Equal(t, org.RoleOwner, r.roleOf(t, r.owner))

	err = r.svc.SetRole(r.as(member, org.RoleMember), r.org.ID, member, string(org.RoleAdmin), uuid.Nil)
	assert.True(t, authz.IsDeniedError(err), "a member holds no members:write, got %T: %v", err, err)

	require.NoError(t, r.svc.SetRole(r.as(admin, org.RoleAdmin), r.org.ID, member, string(org.RoleAdmin), uuid.Nil))
	assert.Equal(t, org.RoleAdmin, r.roleOf(t, member))

	err = r.svc.SetRole(r.as(admin, org.RoleAdmin), r.org.ID, member, "boss", uuid.Nil)
	assert.True(t, org.IsInvalidRoleError(err), "an unknown role is refused, got %T: %v", err, err)
}

func TestLastOwner_CannotLeaveRemoveOrDemote(t *testing.T) {
	r := newRoleOrg(t)
	err := r.svc.SetRole(r.as(r.owner, org.RoleOwner), r.org.ID, r.owner, string(org.RoleAdmin), uuid.Nil)
	le, ok := errors.AsType[*org.LastOwnerError](err)
	require.True(t, ok, "the last owner cannot demote themselves, got %T: %v", err, err)
	assert.Equal(t, "Acme needs at least one owner. Make someone else owner first.", le.ToAppError().Message())
	assert.Equal(t, org.RoleOwner, r.roleOf(t, r.owner))

	err = r.svc.RemoveMember(r.as(r.owner, org.RoleOwner), r.org.ID, r.owner)
	assert.Error(t, err, "the last owner cannot leave")
	assert.Equal(t, org.RoleOwner, r.roleOf(t, r.owner))
}

func TestOwnerTransfer(t *testing.T) {
	r := newRoleOrg(t)
	next := r.seat(t, org.RoleAdmin)

	require.NoError(t, r.svc.SetRole(r.as(r.owner, org.RoleOwner), r.org.ID, next, string(org.RoleOwner), uuid.Nil), "promote first")
	require.NoError(t, r.svc.SetRole(r.as(r.owner, org.RoleOwner), r.org.ID, r.owner, string(org.RoleAdmin), uuid.Nil), "then self-demote")
	assert.Equal(t, org.RoleOwner, r.roleOf(t, next))
	assert.Equal(t, org.RoleAdmin, r.roleOf(t, r.owner))

	err := r.svc.SetRole(r.as(next, org.RoleOwner), r.org.ID, next, string(org.RoleMember), uuid.Nil)
	assert.True(t, org.IsLastOwnerError(err), "the new sole owner is now the last one, got %T: %v", err, err)
}

func TestRemoveMember_NeedsCovers(t *testing.T) {
	r := newRoleOrg(t)
	admin, other := r.seat(t, org.RoleAdmin), r.seat(t, org.RoleAdmin)
	second := r.seat(t, org.RoleOwner)

	err := r.svc.RemoveMember(r.as(admin, org.RoleAdmin), r.org.ID, second)
	assert.Error(t, err, "an admin cannot remove an owner it does not cover")
	assert.Equal(t, org.RoleOwner, r.roleOf(t, second))

	require.NoError(t, r.svc.RemoveMember(r.as(admin, org.RoleAdmin), r.org.ID, other), "an admin covers another admin")
	require.NoError(t, r.svc.RemoveMember(r.as(r.owner, org.RoleOwner), r.org.ID, second), "an owner covers a second owner")
}

func TestSetRole_AuditedInSameTx(t *testing.T) {
	svc, store, a := newAuditedOrgService(t)
	owner := uuid.New()
	o, err := svc.Create(context.Background(), org.CreateRequest{Slug: "acme", Name: "Acme", OwnerID: owner})
	require.NoError(t, err)
	member := uuid.New()
	require.NoError(t, store.SaveMembership(context.Background(), &org.Membership{OrgID: o.ID, UserID: member, Role: org.RoleMember}))
	store.SetUser(member, "bob@example.com", "Bob")

	ctx := fakes.Owner(context.Background(), tenant.Context{OrgID: o.ID, UserID: owner})
	require.NoError(t, svc.SetRole(ctx, o.ID, member, string(org.RoleAdmin), uuid.Nil))
	recs := a.Records()
	require.Len(t, recs, 1)
	assert.Equal(t, "member.role_changed", recs[0].Action)
	assert.Equal(t, map[string]string{"name": "bob@example.com", "email": "bob@example.com", "from_role": "member", "to_role": "admin"}, recs[0].Detail)
	assert.True(t, recs[0].InTx)
}
