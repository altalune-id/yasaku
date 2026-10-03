//go:build integration

package controlplane_test

import (
	"net/url"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	yasakuv1 "altalune.id/yasaku/gen/go/yasaku/v1"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/testutil/pgtest"
	"altalune.id/yasaku/internal/user"
)

func newPgYasakuCfg(t *testing.T) *config.Config {
	t.Helper()
	h := pgtest.New(t)
	sqlDB := h.OpenDB(t)
	require.NoError(t, sqlDB.PingContext(t.Context()))

	cfg := newYasakuCfg(t)
	cfg.DB = db.DBConfig{
		Driver:         db.DriverPostgres,
		DSN:            dsnInSchema(t, h.DSN, h.Schema),
		Schema:         h.Schema,
		TablePrefix:    "yasaku_",
		AutoMigrate:    true,
		AllowBypassRLS: true,
	}
	return cfg
}

// NOTE: pgtest pins only the connection it returns; boot opens its own pool, so the DSN must carry the search_path.
func dsnInSchema(t *testing.T, dsn, schema string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}

// TestScope_ResolvesOnPostgres checks scope resolution carries the org scope that the tenanted org.Store.MembershipOf needs on Postgres.
func TestScope_ResolvesOnPostgres(t *testing.T) {
	h := newYasakuHarnessOn(t, newPgYasakuCfg(t))
	ctx := t.Context()

	w := h.createWallet(ctx, "BCA", &yasakuv1.Money{Amount: "1000000"})
	require.NotEmpty(t, w.GetId())

	list, err := h.walletClient().ListWallets(ctx, connect.NewRequest(&yasakuv1.ListWalletsRequest{
		Target: &yasakuv1.Target{Org: "acme", Project: "main"},
	}))
	require.NoError(t, err)
	require.Len(t, list.Msg.GetWallets(), 1)
	require.Equal(t, "1000000", list.Msg.GetWallets()[0].GetBalance().GetAmount())

	rep, err := h.reportClient().PeriodReport(ctx, connect.NewRequest(&yasakuv1.PeriodReportRequest{}))
	require.NoError(t, err)
	require.NotNil(t, rep.Msg.GetPeriod())
}

func TestScope_NonMemberOrgIsNotFoundOnPostgres(t *testing.T) {
	h := newYasakuHarnessOn(t, newPgYasakuCfg(t))
	ctx := t.Context()

	other, err := h.boot.Users.EnsureFromOIDC(ctx, user.Claims{
		Issuer: testIssuer, Subject: "sub-other", Email: "other@example.com", Name: "Other",
	})
	require.NoError(t, err)
	_, err = h.boot.Orgs.Create(ctx, org.CreateRequest{Slug: "foreign", Name: "Foreign", OwnerID: other.ID})
	require.NoError(t, err)

	for _, slug := range []string{"foreign", "no-such-org"} {
		_, err := h.walletClient().ListWallets(ctx, connect.NewRequest(&yasakuv1.ListWalletsRequest{
			Target: &yasakuv1.Target{Org: slug},
		}))
		require.Error(t, err, slug)
		require.Equal(t, connect.CodeNotFound, connect.CodeOf(err), slug)
	}
}
