package apikey_test

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/apikey"
	"altalune.id/yasaku/internal/platform/authn"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/fakes"
)

func TestAPIKeyCreatedAndRevoked_AuditedInSameTx(t *testing.T) {
	a := &fakes.Audit{}
	svc := apikey.NewService(fakes.NewAPIKey(), apikey.Scheme{}, fakes.PermissiveMembers(), fakes.NewOrgProjects(),
		slog.New(slog.NewTextHandler(io.Discard, nil)), failingUnexpected(t), apikey.WithAudit(fakes.UnitOfWork, a))
	tc := tenant.Context{OrgID: uuid.New(), ProjectID: uuid.New(), UserID: uuid.New()}
	ctx := fakes.Owner(t.Context(), tc)

	k, _, err := svc.Mint(ctx, "ci", []string{authn.ScopeYasakuRead}, nil, in(time.Hour))
	require.NoError(t, err)
	require.NoError(t, svc.Revoke(ctx, k.ID))

	recs := a.Records()
	require.Len(t, recs, 2)
	require.Equal(t, "apikey.created", recs[0].Action)
	require.Equal(t, "apikey.revoked", recs[1].Action)
	for _, r := range recs {
		require.True(t, r.InTx)
		require.Equal(t, k.ID.String(), r.TargetID)
		require.Equal(t, tc.OrgID, r.OrgID)
		require.NotContains(t, r.Detail, "secret", "a key's secret never reaches the audit row")
	}
}
