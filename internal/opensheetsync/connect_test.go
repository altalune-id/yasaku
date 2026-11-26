package opensheetsync_test

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/opensheetsync"
	"altalune.id/yasaku/internal/testutil/fakes"
)

func newConnectService(t *testing.T) (*grantSyncEnv, *opensheetsync.Service) {
	t.Helper()
	e := newGrantSyncEnv(t)
	svc := opensheetsync.NewService(e.store, slog.New(slog.DiscardHandler), apperror.NewReporter(slog.New(slog.DiscardHandler), false).Unexpected,
		opensheetsync.ServiceDeps{
			Mirror: e.mirror, Sealer: e.sealer, Credentials: opensheetsync.NewCredentials(e.sealer, e.grants, e.rp, slog.New(slog.DiscardHandler)),
			Endpoint:   opensheetsync.Endpoint{BaseURL: e.sheets.URL(), AllowPrivateHosts: true, Timeout: 2 * time.Second},
			UnitOfWork: fakes.UnitOfWork, Now: func() time.Time { return e.now },
		})
	return e, svc
}

func targetIDs(c opensheetsync.Choices) []string {
	out := make([]string, 0, len(c.Targets))
	for _, tg := range c.Targets {
		out = append(out, tg.Project.ID+":"+tg.Credential.ID)
	}
	return out
}

func TestChoices_SkipsUnusableCredentials(t *testing.T) {
	e, svc := newConnectService(t)
	e.sheets.AddProject(fakes.OpensheetProject{ID: "A", OrgSlug: "acme", Slug: "books", Name: "Books"},
		fakes.OpensheetCredential{ID: "sa", Kind: "service_account", Email: "bot@iam.gserviceaccount.com"},
		fakes.OpensheetCredential{ID: "stale", Kind: "google_oauth", Status: "reauth_needed", Email: "ana@gmail.com"})
	c, err := svc.Choices(e.ctx)
	require.NoError(t, err)
	require.Empty(t, c.Targets)
	require.Len(t, c.Projects, 1)
	require.Equal(t, opensheetsync.SetupNoGoogle, c.Setup())
}

func TestChoices_PairsEachCredentialWithItsProject(t *testing.T) {
	e, svc := newConnectService(t)
	e.sheets.AddProject(fakes.OpensheetProject{ID: "A", OrgSlug: "acme", Slug: "books", Name: "Books"},
		fakes.OpensheetCredential{ID: "A1", Email: "ana@gmail.com"})
	e.sheets.AddProject(fakes.OpensheetProject{ID: "B", OrgSlug: "beta", Slug: "work", Name: "Work"},
		fakes.OpensheetCredential{ID: "B1", Email: "bo@gmail.com"})
	c, err := svc.Choices(e.ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"A:A1", "B:B1"}, targetIDs(c))
	require.Equal(t, opensheetsync.SetupReady, c.Setup())
}

func TestChoices_SkipsACredentialOfAnotherProject(t *testing.T) {
	e, svc := newConnectService(t)
	e.sheets.AddProject(fakes.OpensheetProject{ID: "A", OrgSlug: "acme", Slug: "books", Name: "Books"},
		fakes.OpensheetCredential{ID: "B1", ProjectID: "B", Email: "bo@gmail.com"})
	c, err := svc.Choices(e.ctx)
	require.NoError(t, err)
	require.Empty(t, c.Targets)
	require.Equal(t, opensheetsync.SetupNoGoogle, c.Setup())
}

func TestChoices_SetupNoAccountWithoutProjects(t *testing.T) {
	e, svc := newConnectService(t)
	c, err := svc.Choices(e.ctx)
	require.NoError(t, err)
	require.Equal(t, opensheetsync.SetupNoAccount, c.Setup())
}

func TestProvision_RefusesForeignOrMalformedTarget(t *testing.T) {
	e, svc := newConnectService(t)
	e.sheets.AddProject(fakes.OpensheetProject{ID: "A", OrgSlug: "acme", Slug: "books", Name: "Books"},
		fakes.OpensheetCredential{ID: "A1", Email: "ana@gmail.com"},
		fakes.OpensheetCredential{ID: "A2", Status: "reauth_needed", Email: "old@gmail.com"})
	e.sheets.AddProject(fakes.OpensheetProject{ID: "B", OrgSlug: "beta", Slug: "work", Name: "Work"},
		fakes.OpensheetCredential{ID: "B1", Email: "bo@gmail.com"})
	for _, pick := range []opensheetsync.Pick{
		{OSProjectID: "A", CredentialID: "B1"},
		{OSProjectID: "A", CredentialID: ""},
		{OSProjectID: "nocolon", CredentialID: ""},
		{OSProjectID: "", CredentialID: "A1"},
		{OSProjectID: "A", CredentialID: "A2"},
	} {
		pick.ProjectName = "Home"
		_, err := svc.Provision(e.ctx, pick)
		require.True(t, opensheetsync.IsInvalidSettingError(err), "%+v: %v", pick, err)
	}
	require.Empty(t, e.sheets.Provisions())
}
