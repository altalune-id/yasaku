package authz_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	"altalune.id/yasaku/internal/platform/authz"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
)

type staffRoles struct {
	roles
	staff map[uuid.UUID]bool
}

func (s *staffRoles) IsStaff(_ context.Context, userID uuid.UUID) (bool, error) {
	return s.staff[userID], nil
}

func TestCan_StaffReadOnly(t *testing.T) {
	staff, outsider := uuid.New(), uuid.New()
	acme, other := uuid.New(), uuid.New()
	src := &staffRoles{roles: roles{byUser: map[uuid.UUID]string{}}, staff: map[uuid.UUID]bool{staff: true}}
	live := &session.Support{OrgID: acme, Reason: "ticket 42", Until: time.Now().Add(time.Hour), EventID: uuid.New()}

	held := func(t *testing.T, p session.Principal) context.Context {
		t.Helper()
		ctx, err := authz.NewResolver(src).Bind(context.Background(), p, tenant.Context{OrgID: acme, UserID: p.UserID, ReadOnly: true})
		require.NoError(t, err)
		return ctx
	}

	t.Run("staff in a live session read every resource and write none", func(t *testing.T) {
		ctx := held(t, session.Principal{UserID: staff, Source: session.SourceOIDC, Support: live})
		for _, d := range authz.Catalogue() {
			err := authz.Can(ctx, d.Permission)
			if strings.HasSuffix(string(d.Permission), ":read") {
				assert.NoError(t, err, "%s", d.Permission)
				continue
			}
			assert.True(t, authz.IsDeniedError(err), "%s must be refused in a support session", d.Permission)
		}
	})

	for name, p := range map[string]session.Principal{
		"expired session":      {UserID: staff, Source: session.SourceOIDC, Support: &session.Support{OrgID: acme, Until: time.Now().Add(-time.Minute)}},
		"session for another":  {UserID: staff, Source: session.SourceOIDC, Support: &session.Support{OrgID: other, Until: time.Now().Add(time.Hour)}},
		"forged by non-staff":  {UserID: outsider, Source: session.SourceOIDC, Support: live},
		"no session":           {UserID: staff, Source: session.SourceOIDC},
		"token never supports": {UserID: staff, Source: session.SourceToken, Scopes: []string{"yasaku:read"}, Support: live},
	} {
		t.Run(name, func(t *testing.T) {
			assert.True(t, authz.IsDeniedError(authz.Can(held(t, p), authz.WalletsRead)))
		})
	}
}

func TestSupportErrors_MapToCodes(t *testing.T) {
	ro := (&authz.SupportReadOnlyError{Org: "Acme"}).ToAppError()
	assert.Equal(t, "SUP004", ro.Code())
	assert.Equal(t, codes.PermissionDenied, ro.GRPCCode())
	assert.Contains(t, ro.Message(), "Acme")
	assert.True(t, authz.IsSupportReadOnlyError(fmt.Errorf("wrapped: %w", &authz.SupportReadOnlyError{})))

	reason := (&authz.SupportReasonRequiredError{}).ToAppError()
	assert.Equal(t, "SUP001", reason.Code())
	assert.Equal(t, codes.InvalidArgument, reason.GRPCCode())
	assert.True(t, authz.IsSupportReasonRequiredError(fmt.Errorf("wrapped: %w", &authz.SupportReasonRequiredError{})))
}
