package authz_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/platform/authn"
	"altalune.id/yasaku/internal/platform/authz"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
)

type roles struct {
	byUser map[uuid.UUID]string
	calls  int
}

func (r *roles) RoleOf(_ context.Context, _, userID uuid.UUID) (string, uuid.UUID, string, error) {
	r.calls++
	return r.byUser[userID], uuid.Nil, "Acme", nil
}

func (*roles) CustomPermissions(context.Context, uuid.UUID) ([]authz.Permission, error) {
	return nil, nil
}
func (*roles) IsStaff(context.Context, uuid.UUID) (bool, error)   { return false, nil }
func (*roles) OrgName(context.Context, uuid.UUID) (string, error) { return "Acme", nil }

func bind(t *testing.T, src *roles, p session.Principal) context.Context {
	t.Helper()
	ctx, err := authz.NewResolver(src).Bind(context.Background(), p, tenant.Context{OrgID: uuid.New(), UserID: p.UserID})
	require.NoError(t, err)
	return ctx
}

func TestCan_Table(t *testing.T) {
	owner, admin, member, gone := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	src := &roles{byUser: map[uuid.UUID]string{owner: "owner", admin: "admin", member: "member"}}
	tests := []struct {
		name    string
		p       session.Principal
		perm    authz.Permission
		allowed bool
		side    authz.Side
	}{
		{"owner holds roles:write", session.Principal{UserID: owner, Source: session.SourceOIDC}, authz.RolesWrite, true, ""},
		{"admin lacks roles:write", session.Principal{UserID: admin, Source: session.SourceOIDC}, authz.RolesWrite, false, authz.SideRole},
		{"admin closes periods", session.Principal{UserID: admin, Source: session.SourceOIDC}, authz.PeriodsClose, true, ""},
		{"member writes wallets", session.Principal{UserID: member, Source: session.SourceOIDC}, authz.WalletsWrite, true, ""},
		{"member refused periods:close", session.Principal{UserID: member, Source: session.SourceOIDC}, authz.PeriodsClose, false, authz.SideRole},
		{"a non-member holds nothing", session.Principal{UserID: gone, Source: session.SourceOIDC}, authz.WalletsRead, false, authz.SideRole},
		{"read token reads", session.Principal{UserID: owner, Source: session.SourceToken, Scopes: []string{authn.ScopeYasakuRead}}, authz.WalletsRead, true, ""},
		{"read token refused wallets:write", session.Principal{UserID: owner, Source: session.SourceToken, Scopes: []string{authn.ScopeYasakuRead}}, authz.WalletsWrite, false, authz.SideScope},
		{"token never holds a console permission", session.Principal{UserID: owner, Source: session.SourceToken, Scopes: []string{authn.ScopeYasakuWrite, authn.ScopeAPIKeysWrite}}, authz.APIKeysWrite, false, authz.SideScope},
		{"write token as member refused periods:close", session.Principal{UserID: member, Source: session.SourceToken, Scopes: []string{authn.ScopeYasakuWrite}}, authz.PeriodsClose, false, authz.SideRole},
		{"personal key bounded by owner role", session.Principal{KeyID: uuid.New(), Source: session.SourceAPIKey, KeyKind: session.KeyKindPersonal, KeyOwnerID: member, Scopes: []string{authn.ScopeYasakuWrite}}, authz.PeriodsClose, false, authz.SideRole},
		{"org key bounded by creator", session.Principal{KeyID: uuid.New(), Source: session.SourceAPIKey, KeyKind: session.KeyKindOrg, KeyOwnerID: admin, Scopes: []string{authn.ScopeYasakuWrite}}, authz.PeriodsClose, true, ""},
		{"org key of a departed creator holds nothing", session.Principal{KeyID: uuid.New(), Source: session.SourceAPIKey, KeyKind: session.KeyKindOrg, KeyOwnerID: gone, Scopes: []string{authn.ScopeYasakuWrite}}, authz.WalletsWrite, false, authz.SideRole},
		{"NULL-creator key keeps admin rights", session.Principal{KeyID: uuid.New(), Source: session.SourceAPIKey, KeyKind: session.KeyKindProject, Scopes: []string{authn.ScopeYasakuWrite}}, authz.PeriodsClose, true, ""},
		{"NULL-creator key refused members:write", session.Principal{KeyID: uuid.New(), Source: session.SourceAPIKey, KeyKind: session.KeyKindProject, Scopes: []string{authn.ScopeYasakuWrite}}, authz.MembersWrite, false, authz.SideScope},
		{"owner's key refused a console permission", session.Principal{KeyID: uuid.New(), Source: session.SourceAPIKey, KeyKind: session.KeyKindOrg, KeyOwnerID: owner, Scopes: []string{authn.ScopeYasakuWrite, authn.ScopeAPIKeysWrite}}, authz.APIKeysWrite, false, authz.SideScope},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := authz.Can(bind(t, src, tt.p), tt.perm)
			if tt.allowed {
				require.NoError(t, err)
				return
			}
			de, ok := errors.AsType[*authz.DeniedError](err)
			require.True(t, ok, "want *DeniedError, got %T: %v", err, err)
			assert.Equal(t, tt.side, de.Side)
			assert.Equal(t, tt.perm, de.Permission)
		})
	}
}

func TestDenied_MessagesBySide(t *testing.T) {
	member := uuid.New()
	src := &roles{byUser: map[uuid.UUID]string{member: "member"}}
	err := authz.Can(bind(t, src, session.Principal{UserID: member, Source: session.SourceOIDC}), authz.PeriodsClose)
	de, _ := errors.AsType[*authz.DeniedError](err)
	require.NotNil(t, de)
	assert.Equal(t, "Your role in Acme can't close periods. Ask an owner or admin.", de.ToAppError().Message())

	err = authz.Can(bind(t, src, session.Principal{UserID: member, Source: session.SourceToken, Scopes: []string{authn.ScopeYasakuRead}}), authz.WalletsWrite)
	de, _ = errors.AsType[*authz.DeniedError](err)
	require.NotNil(t, de)
	assert.Equal(t, "This connection can only read. Reconnect it with write access.", de.ToAppError().Message())
}

func TestCan_NoBindRefuses(t *testing.T) {
	err := authz.Can(context.Background(), authz.WalletsRead)
	assert.True(t, authz.IsDeniedError(err), "a context no seam bound holds nothing, got %T: %v", err, err)
}

func TestAsSystem_HoldsEverything(t *testing.T) {
	ctx := authz.AsSystem(context.Background())
	for _, d := range authz.Catalogue() {
		require.NoError(t, authz.Can(ctx, d.Permission), d.Permission)
	}
}

func TestBuiltin_RolesNest(t *testing.T) {
	owner, admin, member := authz.Builtin("owner"), authz.Builtin("admin"), authz.Builtin("member")
	assert.Len(t, owner, len(authz.Catalogue()), "owner holds the whole catalogue")
	assert.True(t, authz.Covers(owner, admin...))
	assert.True(t, authz.Covers(admin, member...))
	assert.False(t, authz.Covers(admin, authz.RolesWrite))
	assert.False(t, slices.Contains(member, authz.MembersWrite))
	assert.Empty(t, authz.Builtin("stranger"))
}

func TestResolve_OnceAtBind(t *testing.T) {
	owner := uuid.New()
	src := &roles{byUser: map[uuid.UUID]string{owner: "owner"}}
	ctx := bind(t, src, session.Principal{UserID: owner, Source: session.SourceOIDC})
	for range 3 {
		require.NoError(t, authz.Can(ctx, authz.WalletsWrite))
	}
	assert.Equal(t, 1, src.calls, "Can is a pure read of the held set; only Bind asks the role source")
}

func TestCatalogue_TwelvePermissions(t *testing.T) {
	assert.Len(t, authz.Catalogue(), 12)
	for _, d := range authz.Catalogue() {
		assert.NotEmpty(t, d.LabelKey, d.Permission)
		if d.Holder == authz.HolderConsole {
			assert.Empty(t, d.Scope, "%s is console-only, so no scope admits it", d.Permission)
		}
	}
}

func TestWriteScopeAdmitsReads(t *testing.T) {
	owner := uuid.New()
	src := &roles{byUser: map[uuid.UUID]string{owner: "owner"}}
	for name, p := range map[string]session.Principal{
		"token": {UserID: owner, Source: session.SourceToken, Scopes: []string{authn.ScopeYasakuWrite}},
		"key":   {KeyID: uuid.New(), Source: session.SourceAPIKey, KeyKind: session.KeyKindOrg, KeyOwnerID: owner, Scopes: []string{authn.ScopeYasakuWrite}},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := bind(t, src, p)
			require.NoError(t, authz.Can(ctx, authz.WalletsRead), "a write-only credential reads what it writes")
			require.NoError(t, authz.Can(ctx, authz.TransactionsWrite))
		})
	}
}
