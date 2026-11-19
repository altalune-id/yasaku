package org_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
)

// SECURITY: every manager-only write asks this one gate, so a member must be refused whichever credential carries them.
func TestRequireCallerManager(t *testing.T) {
	t.Parallel()
	svc, store := newTestService(t, false)
	ctx := context.Background()
	orgID := uuid.New()
	seat := func(role org.Role) uuid.UUID {
		t.Helper()
		userID := uuid.New()
		m, err := org.NewMembership(orgID, userID, role)
		require.NoError(t, err)
		require.NoError(t, store.SaveMembership(ctx, m))
		return userID
	}
	manager, member := seat(org.RoleAdmin), seat(org.RoleMember)

	person := func(userID uuid.UUID) context.Context {
		return tenant.Into(ctx, tenant.Context{OrgID: orgID, UserID: userID})
	}
	key := func(kind session.KeyKind, owner uuid.UUID) context.Context {
		kctx := session.PrincipalInto(ctx, session.Principal{KeyID: uuid.New(), KeyKind: kind, KeyOwnerID: owner})
		return tenant.Into(kctx, tenant.Context{OrgID: orgID})
	}
	tests := []struct {
		name  string
		ctx   context.Context
		allow bool
	}{
		{"a manager", person(manager), true},
		{"a member", person(member), false},
		{"an org key", key(session.KeyKindOrg, uuid.Nil), true},
		{"a project key", key(session.KeyKindProject, uuid.Nil), true},
		{"a personal key of a manager", key(session.KeyKindPersonal, manager), true},
		{"a personal key of a member", key(session.KeyKindPersonal, member), false},
		{"a personal key with no owner", key(session.KeyKindPersonal, uuid.Nil), false},
		{"a key of no known kind", key("", uuid.Nil), false},
		{"a key of an unknown kind", key("robot", manager), false},
		{"no person and no key", tenant.Into(ctx, tenant.Context{OrgID: orgID}), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.RequireCallerManager(tt.ctx)
			if tt.allow {
				require.NoError(t, err)
				return
			}
			require.True(t, org.IsNotManagerError(err), "got %T: %v", err, err)
		})
	}
	require.Error(t, svc.RequireCallerManager(ctx), "a context without a tenant scope refuses")
}

// SECURITY: the helper itself refuses a personal key with no owner, rather than leaning on RequireManager refusing a nil user.
func TestManagerCaller_APersonalKeyWithNoOwnerIsRefused(t *testing.T) {
	t.Parallel()
	orgID := uuid.New()
	ctx := session.PrincipalInto(context.Background(), session.Principal{KeyID: uuid.New(), KeyKind: session.KeyKindPersonal})
	ctx = tenant.Into(ctx, tenant.Context{OrgID: orgID})
	_, _, managerKey, err := org.ManagerCaller(ctx)
	require.False(t, managerKey)
	require.True(t, org.IsNotManagerError(err), "got %T: %v", err, err)
}

// SECURITY: a write naming another org is refused even for a manager of the tenant org, and even for an org key.
func TestRequireCallerManagerOf_RefusesAnOrgOtherThanTheTenants(t *testing.T) {
	t.Parallel()
	svc, store := newTestService(t, false)
	ctx := context.Background()
	orgID, other, manager := uuid.New(), uuid.New(), uuid.New()
	m, err := org.NewMembership(orgID, manager, org.RoleAdmin)
	require.NoError(t, err)
	require.NoError(t, store.SaveMembership(ctx, m))
	person := tenant.Into(ctx, tenant.Context{OrgID: orgID, UserID: manager})
	key := tenant.Into(session.PrincipalInto(ctx, session.Principal{KeyID: uuid.New(), KeyKind: session.KeyKindOrg}), tenant.Context{OrgID: orgID})

	require.NoError(t, svc.RequireCallerManagerOf(person, orgID))
	requireOrgMismatch(t, svc.RequireCallerManagerOf(person, other), other)
	require.NoError(t, svc.RequireCallerManagerOf(key, orgID))
	requireOrgMismatch(t, svc.RequireCallerManagerOf(key, other), other)
}

func requireOrgMismatch(t *testing.T, err error, target uuid.UUID) {
	t.Helper()
	require.True(t, org.IsOrgMismatchError(err), "got %T: %v", err, err)
	require.False(t, org.IsNotManagerError(err), "an org mismatch is not a role refusal")
	ae, ok := apperror.AsAppError(err)
	require.True(t, ok)
	require.Equal(t, apperror.CodeOrgMismatch, ae.Code())
	require.Equal(t, codes.PermissionDenied, ae.GRPCCode())
	require.Contains(t, ae.Message(), target.String())
	require.Contains(t, ae.Message(), "different organization")
}
