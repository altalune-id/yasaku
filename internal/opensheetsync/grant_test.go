package opensheetsync_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/altalune-id/oidcrp"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/opensheetsync"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/sealer"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/money"
)

// NOTE: grantFixture is one GrantStore under test; newCreds builds what one process holds, so a second call stands in for a second process.
type grantFixture struct {
	grants opensheetsync.GrantStore
	tc     tenant.Context
	other  tenant.Context
	sealer sealer.Sealer
	rp     *fakes.OpensheetRefresher
}

func (f grantFixture) ctx() context.Context { return fakes.Owner(context.Background(), f.tc) }

func (f grantFixture) newCreds() *opensheetsync.Credentials {
	return opensheetsync.NewCredentials(f.sealer, f.grants, f.rp, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func (f grantFixture) link() *opensheetsync.Link {
	l := opensheetsync.NewLink(f.tc.OrgID, f.tc.ProjectID, f.tc.UserID, uuid.Nil, time.Now())
	l.Credential = opensheetsync.CredentialGrant
	return l
}

func (f grantFixture) seed(t *testing.T, tc tenant.Context, access string, expiresIn time.Duration) {
	t.Helper()
	g, err := opensheetsync.NewGrant(f.sealer, tc.OrgID, tc.ProjectID, tc.UserID, oidcrp.TokenSet{
		AccessToken: access, RefreshToken: "refresh-0", ExpiresAt: time.Now().Add(expiresIn), Scopes: []string{"sheets:read", "offline_access"},
	}, time.Now())
	require.NoError(t, err)
	require.NoError(t, f.grants.SaveGrant(fakes.Owner(context.Background(), tc), g))
}

func (f grantFixture) stored(t *testing.T) (*opensheetsync.Grant, string) {
	t.Helper()
	g, err := f.grants.GrantByProject(f.ctx(), f.tc.OrgID, f.tc.ProjectID)
	require.NoError(t, err)
	rt, err := g.RefreshToken(f.sealer)
	require.NoError(t, err)
	return g, rt
}

func newSQLiteGrantFixture(t *testing.T) grantFixture {
	t.Helper()
	sf, pool := newSQLiteFixture(t)
	return grantFixture{
		grants: opensheetsync.NewGrantStore(db.DBConfig{Driver: db.DriverSQLite, TablePrefix: config.Defaults().DB.TablePrefix}, pool, nil),
		tc:     sf.tc, other: sf.other, sealer: newSealer(t), rp: &fakes.OpensheetRefresher{},
	}
}

func newFakeGrantFixture(t *testing.T) grantFixture {
	t.Helper()
	return grantFixture{
		grants: fakes.NewOpensheetGrants(),
		tc:     tenant.Context{OrgID: uuid.New(), ProjectID: uuid.New(), UserID: uuid.New()},
		other:  tenant.Context{OrgID: uuid.New(), ProjectID: uuid.New(), UserID: uuid.New()},
		sealer: newSealer(t), rp: &fakes.OpensheetRefresher{},
	}
}

// NOTE: every caller shares one refresh: processes holds one Credentials per simulated process.
func runSingleFlightRefresh(t *testing.T, f grantFixture, processes int) {
	t.Helper()
	f.seed(t, f.tc, "access-0", 10*time.Second)
	f.rp.Delay = 150 * time.Millisecond
	creds := make([]*opensheetsync.Credentials, processes)
	for i := range creds {
		creds[i] = f.newCreds()
	}
	const callers = 8
	got := make([]string, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Go(func() { got[i], errs[i] = creds[i%processes].Bearer(f.ctx(), f.link()) })
	}
	wg.Wait()
	for i := range callers {
		require.NoError(t, errs[i])
		require.Equal(t, "access-1", got[i], "caller %d gets the one refreshed token", i)
	}
	require.Len(t, f.rp.Refreshes(), 1, "one refresh for every caller")
	_, rt := f.stored(t)
	require.Equal(t, "refresh-1", rt, "the rotated refresh token is saved")

	again, err := creds[0].Bearer(f.ctx(), f.link())
	require.NoError(t, err)
	require.Equal(t, "access-1", again)
	require.Len(t, f.rp.Refreshes(), 1, "a fresh access token is used as it is")
}

func runRotatedTokenSavedOnError(t *testing.T, f grantFixture) {
	t.Helper()
	f.seed(t, f.tc, "access-0", 10*time.Second)
	f.rp.Next = func(context.Context, oidcrp.TokenSet) (*oidcrp.TokenSet, error) {
		return &oidcrp.TokenSet{RefreshToken: "refresh-rotated"}, errors.New("the token response failed a check")
	}
	_, err := f.newCreds().Bearer(f.ctx(), f.link())
	require.Error(t, err)
	require.False(t, opensheetsync.IsReconnectNeededError(err), "a failed check is worth a retry, not a reconnect")
	g, rt := f.stored(t)
	require.Equal(t, "refresh-rotated", rt, "the issuer spent the old refresh token, so the rotated one is kept")
	require.Equal(t, opensheetsync.GrantOK, g.Status)
}

func runInvalidGrantMarksReconnect(t *testing.T, f grantFixture) {
	t.Helper()
	f.seed(t, f.tc, "access-0", 10*time.Second)
	f.rp.Next = func(context.Context, oidcrp.TokenSet) (*oidcrp.TokenSet, error) {
		return nil, &oidcrp.Error{Reason: oidcrp.ReasonTokenExchange, OAuthError: "invalid_grant"}
	}
	creds := f.newCreds()
	_, err := creds.Bearer(f.ctx(), f.link())
	require.True(t, opensheetsync.IsReconnectNeededError(err), "got %v", err)
	app, ok := apperror.AsAppError(err)
	require.True(t, ok)
	require.Equal(t, apperror.CodeOpensheetReconnectNeeded, app.Code())
	require.Equal(t, "OSL017", app.Code())
	require.Equal(t, codes.FailedPrecondition, app.GRPCCode())
	g, _ := f.stored(t)
	require.Equal(t, opensheetsync.GrantReconnectNeeded, g.Status)

	_, err = creds.Bearer(f.ctx(), f.link())
	require.True(t, opensheetsync.IsReconnectNeededError(err))
	require.Len(t, f.rp.Refreshes(), 1, "a grant needing a reconnect is never refreshed again")
}

// NOTE: the delete runs on the refresh's own context, inside the Postgres row lock, so only the write-back's filter can keep the grant gone.
func runRefreshNeverRecreatesADeletedGrant(t *testing.T, f grantFixture) {
	t.Helper()
	for name, fail := range map[string]bool{"rotated": false, "invalid_grant": true} {
		f.seed(t, f.tc, "access-0", 10*time.Second)
		f.rp.Next = func(ctx context.Context, _ oidcrp.TokenSet) (*oidcrp.TokenSet, error) {
			require.NoError(t, f.grants.DeleteGrant(ctx, f.tc.OrgID, f.tc.ProjectID))
			if fail {
				return nil, &oidcrp.Error{Reason: oidcrp.ReasonTokenExchange, OAuthError: "invalid_grant"}
			}
			return &oidcrp.TokenSet{AccessToken: "access-1", RefreshToken: "refresh-1", ExpiresAt: time.Now().Add(time.Hour)}, nil
		}
		_, err := f.newCreds().Bearer(f.ctx(), f.link())
		require.True(t, opensheetsync.IsReconnectNeededError(err), "%s: got %v", name, err)
		_, err = f.grants.GrantByProject(f.ctx(), f.tc.OrgID, f.tc.ProjectID)
		require.True(t, opensheetsync.IsGrantNotFoundError(err), "%s: the write-back must not bring the deleted grant back: %v", name, err)
	}
}

func TestGrantSource_RefreshNeverRecreatesADeletedGrant(t *testing.T) {
	runRefreshNeverRecreatesADeletedGrant(t, newSQLiteGrantFixture(t))
	runRefreshNeverRecreatesADeletedGrant(t, newFakeGrantFixture(t))
}

func TestGrantSource_SingleFlightRefresh(t *testing.T) {
	runSingleFlightRefresh(t, newSQLiteGrantFixture(t), 1)
}

func TestGrantSource_RotatedTokenSavedOnError(t *testing.T) {
	runRotatedTokenSavedOnError(t, newSQLiteGrantFixture(t))
	runRotatedTokenSavedOnError(t, newFakeGrantFixture(t))
}

func TestGrantSource_InvalidGrantMarksReconnect(t *testing.T) {
	runInvalidGrantMarksReconnect(t, newSQLiteGrantFixture(t))
	runInvalidGrantMarksReconnect(t, newFakeGrantFixture(t))
}

func TestGrantSource_NoGrantNeedsReconnect(t *testing.T) {
	f := newSQLiteGrantFixture(t)
	_, err := f.newCreds().Bearer(f.ctx(), f.link())
	require.True(t, opensheetsync.IsReconnectNeededError(err), "got %v", err)
	require.Empty(t, f.rp.Refreshes())
}

func TestGrantStore_AnotherOrgsGrantIsRefused(t *testing.T) {
	for name, f := range map[string]grantFixture{"sqlite": newSQLiteGrantFixture(t), "fake": newFakeGrantFixture(t)} {
		f.seed(t, f.tc, "access-0", time.Hour)
		hijack, err := opensheetsync.NewGrant(f.sealer, f.other.OrgID, f.tc.ProjectID, f.other.UserID, oidcrp.TokenSet{RefreshToken: "evil"}, time.Now())
		require.NoError(t, err)
		err = f.grants.SaveGrant(fakes.Owner(context.Background(), f.other), hijack)
		require.True(t, opensheetsync.IsGrantNotFoundError(err), "%s: got %v", name, err)
		_, rt := f.stored(t)
		require.Equal(t, "refresh-0", rt, "%s: the owner's grant is untouched", name)
		_, err = f.grants.GrantByProject(fakes.Owner(context.Background(), f.other), f.other.OrgID, f.tc.ProjectID)
		require.True(t, opensheetsync.IsGrantNotFoundError(err), "%s: another org reads no grant", name)
	}
}

func TestMemberRemoved_RevokesGrant(t *testing.T) {
	for name, f := range map[string]grantFixture{"sqlite": newSQLiteGrantFixture(t), "fake": newFakeGrantFixture(t)} {
		f.seed(t, f.tc, "access-0", time.Hour)
		f.rp.RevokeErr = errors.New("opensheet is down")
		orgOnly := fakes.Owner(context.Background(), tenant.Context{OrgID: f.tc.OrgID, UserID: uuid.New()})
		require.NoError(t, f.newCreds().RevokeMember(orgOnly, f.tc.OrgID, f.other.UserID), "%s", name)
		_, err := f.grants.GrantByProject(f.ctx(), f.tc.OrgID, f.tc.ProjectID)
		require.NoError(t, err, "%s: another member's grant stays", name)
		require.Empty(t, f.rp.Revoked(), "%s", name)

		require.NoError(t, f.newCreds().RevokeMember(orgOnly, f.tc.OrgID, f.tc.UserID), "%s: a failed remote revoke never blocks the removal", name)
		_, err = f.grants.GrantByProject(f.ctx(), f.tc.OrgID, f.tc.ProjectID)
		require.True(t, opensheetsync.IsGrantNotFoundError(err), "%s: the member's grant is gone: %v", name, err)
		require.Equal(t, []string{"refresh-0"}, f.rp.Revoked(), "%s: the member's refresh token is revoked", name)
	}
}

func TestKeyLinkUnchanged(t *testing.T) {
	e := newSyncEnv(t)
	rp := &fakes.OpensheetRefresher{}
	creds := opensheetsync.NewCredentials(e.sealer, fakes.NewOpensheetGrants(), rp, slog.New(slog.DiscardHandler))
	l := e.link(t)
	require.Equal(t, opensheetsync.CredentialAPIKey, l.Credential, "a link saved with a key is a key link")
	bearer, err := creds.Bearer(e.ctx, l)
	require.NoError(t, err)
	require.Equal(t, goodKey, bearer)

	syncer := opensheetsync.NewSyncer(e.store, slog.New(slog.DiscardHandler), apperror.NewReporter(slog.New(slog.DiscardHandler), false).Unexpected,
		opensheetsync.SyncerDeps{
			Sealer: e.sealer, Credentials: creds, Source: e.source, Now: func() time.Time { return e.now },
			Endpoint: opensheetsync.Endpoint{BaseURL: e.sheets.URL(), AllowPrivateHosts: true, Timeout: 2 * time.Second},
		})
	r := e.tx(t, "lunch")
	require.NoError(t, syncer.Sync(e.ctx, e.tc.ProjectID, []opensheetsync.Ref{r}))
	_, ok := e.sheets.Row("yasaku-transactions", r.ID.String())
	require.True(t, ok, "a key link syncs with its key")
	require.Empty(t, rp.Refreshes(), "a key link never touches a grant")
}

type grantSyncEnv struct {
	*serviceEnv
	grants *fakes.OpensheetGrants
	rp     *fakes.OpensheetRefresher
	source *stubSource
	syncer *opensheetsync.Syncer
}

func newGrantSyncEnv(t *testing.T) *grantSyncEnv {
	t.Helper()
	e := &grantSyncEnv{serviceEnv: newServiceEnv(t), grants: fakes.NewOpensheetGrants(), rp: &fakes.OpensheetRefresher{},
		source: &stubSource{txs: map[uuid.UUID]opensheetsync.TransactionFacts{}, wallet: map[uuid.UUID]opensheetsync.WalletFacts{}, category: map[uuid.UUID]opensheetsync.CategoryFacts{}}}
	addContractSheets(e.sheets, true)
	e.sheets.SetToken("access-0")
	f := grantFixture{grants: e.grants, tc: e.tc, sealer: e.sealer, rp: e.rp}
	f.seed(t, e.tc, "access-0", time.Hour)
	l := f.link()
	l.Configure(opensheetsync.Settings{OSOrg: "acme", OSProject: "home", Sheets: opensheetsync.DefaultSheetSlugs()}, nil, "", e.now)
	require.NoError(t, l.Enable(e.now))
	require.NoError(t, e.store.SaveLink(e.ctx, l))
	e.syncer = opensheetsync.NewSyncer(e.store, slog.New(slog.DiscardHandler), apperror.NewReporter(slog.New(slog.DiscardHandler), false).Unexpected,
		opensheetsync.SyncerDeps{
			Sealer: e.sealer, Credentials: f.newCreds(), Source: e.source, Now: func() time.Time { return e.now },
			Endpoint: opensheetsync.Endpoint{BaseURL: e.sheets.URL(), AllowPrivateHosts: true, Timeout: 2 * time.Second},
		})
	return e
}

func (e *grantSyncEnv) tx(t *testing.T) opensheetsync.Ref {
	t.Helper()
	id := uuid.New()
	e.source.mu.Lock()
	e.source.txs[id] = opensheetsync.TransactionFacts{ID: id, Kind: "expense", Amount: money.New(4_000_000, money.IDR),
		OccurredAt: e.now, WalletID: uuid.New(), WalletName: "BCA", Note: "lunch", UpdatedAt: e.now}
	e.source.mu.Unlock()
	r := ref(opensheetsync.EntityTransaction, id)
	require.NoError(t, e.mirror.Mark(e.ctx, r))
	return r
}

func TestSync_GrantLinkRefreshesOnceWhenOpensheetSaysExpired(t *testing.T) {
	e := newGrantSyncEnv(t)
	e.rp.Next = func(_ context.Context, prev oidcrp.TokenSet) (*oidcrp.TokenSet, error) {
		e.sheets.SetToken("access-1")
		return &oidcrp.TokenSet{AccessToken: "access-1", RefreshToken: "refresh-1", ExpiresAt: time.Now().Add(time.Hour), Scopes: prev.Scopes}, nil
	}
	e.sheets.FailNext("POST", "yasaku-transactions", 401, "TKN001", 1)
	r := e.tx(t)
	require.NoError(t, e.syncer.Sync(e.ctx, e.tc.ProjectID, []opensheetsync.Ref{r}))
	_, ok := e.sheets.Row("yasaku-transactions", r.ID.String())
	require.True(t, ok, "the row lands with the refreshed token")
	require.Len(t, e.rp.Refreshes(), 1)

	e.sheets.FailNext("POST", "yasaku-transactions", 401, "TKN001", 2)
	err := e.syncer.Sync(e.ctx, e.tc.ProjectID, []opensheetsync.Ref{e.tx(t)})
	require.True(t, opensheetsync.IsSyncRefusedError(err), "a second expiry after a refresh is refused, got %v", err)
	require.Len(t, e.rp.Refreshes(), 2, "one refresh per job, never a loop")
}

func TestSync_GrantNeedingReconnectRefusesWithOSL017(t *testing.T) {
	e := newGrantSyncEnv(t)
	g, err := e.grants.GrantByProject(e.ctx, e.tc.OrgID, e.tc.ProjectID)
	require.NoError(t, err)
	g.Status = opensheetsync.GrantReconnectNeeded
	require.NoError(t, e.grants.SaveGrant(e.ctx, g))
	err = e.syncer.Sync(e.ctx, e.tc.ProjectID, []opensheetsync.Ref{e.tx(t)})
	require.True(t, opensheetsync.IsSyncRefusedError(err), "got %v", err)
	require.True(t, opensheetsync.IsReconnectNeededError(err))
	l, err := e.store.LinkByProject(e.ctx, e.tc.OrgID, e.tc.ProjectID)
	require.NoError(t, err)
	require.Equal(t, "OSL017", opensheetsync.ErrorCode(l.LastError), "the link records why: %q", l.LastError)
	require.Zero(t, e.sheets.CountRequests("POST", "yasaku-transactions"), "nothing reaches opensheet")
}

func TestService_DeleteRemovesAndRevokesTheGrant(t *testing.T) {
	e := newGrantSyncEnv(t)
	e.rp.RevokeErr = errors.New("opensheet is down")
	svc := opensheetsync.NewService(e.store, slog.New(slog.DiscardHandler), apperror.NewReporter(slog.New(slog.DiscardHandler), false).Unexpected,
		opensheetsync.ServiceDeps{
			Mirror: e.mirror, Sealer: e.sealer, Credentials: opensheetsync.NewCredentials(e.sealer, e.grants, e.rp, slog.New(slog.DiscardHandler)),
			UnitOfWork: fakes.UnitOfWork, Now: func() time.Time { return e.now },
		})
	require.NoError(t, svc.Delete(e.ctx), "a failed upstream revoke never fails the disconnect")
	_, err := e.store.LinkByProject(e.ctx, e.tc.OrgID, e.tc.ProjectID)
	require.True(t, opensheetsync.IsLinkNotFoundError(err))
	_, err = e.grants.GrantByProject(e.ctx, e.tc.OrgID, e.tc.ProjectID)
	require.True(t, opensheetsync.IsGrantNotFoundError(err))
	require.Equal(t, []string{"refresh-0"}, e.rp.Revoked())
	require.True(t, opensheetsync.IsLinkNotFoundError(svc.Delete(e.ctx)), "nothing left to disconnect")
}

func TestGrantStore_ConnectedByJoinsTheUnitOfWork(t *testing.T) {
	sf, pool := newSQLiteFixture(t)
	cfg := db.DBConfig{Driver: db.DriverSQLite, TablePrefix: config.Defaults().DB.TablePrefix}
	f := grantFixture{grants: opensheetsync.NewGrantStore(cfg, pool, nil), tc: sf.tc, sealer: newSealer(t), rp: &fakes.OpensheetRefresher{}}
	f.seed(t, f.tc, "access-0", time.Hour)
	ctx, cancel := context.WithTimeout(f.ctx(), 3*time.Second)
	defer cancel()
	var got []*opensheetsync.Grant
	err := tenant.NewUnitOfWork(cfg, pool, nil)(ctx, func(ctx context.Context) error {
		if err := f.grants.DeleteGrant(ctx, f.tc.OrgID, f.tc.ProjectID); err != nil {
			return err
		}
		var err error
		got, err = f.grants.GrantsConnectedBy(ctx, f.tc.OrgID, f.tc.UserID)
		return err
	})
	require.NoError(t, err)
	require.Empty(t, got, "a read inside a unit of work uses its transaction, so it sees the delete made there")
}
