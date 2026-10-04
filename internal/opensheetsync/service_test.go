package opensheetsync_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/opensheetsync"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/queue"
	"altalune.id/yasaku/internal/platform/sealer"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/fakes"
)

const goodKey = "osk_live_0123456789abcd"

type serviceEnv struct {
	*mirrorEnv
	sheets  *fakes.Opensheet
	members *fakes.Members
	sealer  sealer.Sealer
	logs    *lockedBuffer
	svc     *opensheetsync.Service
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func addContractSheets(f *fakes.Opensheet, writable bool) {
	for _, tab := range opensheetsync.Contract() {
		f.AddSheet(tab.DefaultSlug, fakes.OpensheetSheet{Columns: tab.Columns, Writable: writable})
	}
}

func newSealer(t *testing.T) sealer.Sealer {
	t.Helper()
	key, err := sealer.GenerateKey()
	require.NoError(t, err)
	sl, err := sealer.New(key)
	require.NoError(t, err)
	return sl
}

func newServiceEnv(t *testing.T) *serviceEnv {
	t.Helper()
	m := newMirrorEnv(t)
	e := &serviceEnv{
		mirrorEnv: m, sheets: fakes.NewOpensheet(t, "acme", "home", goodKey), members: fakes.NewMembers(),
		sealer: newSealer(t), logs: &lockedBuffer{},
	}
	e.members.SeatManager(m.tc.OrgID, m.tc.UserID)
	e.svc = e.service(e.sealer, m.mirror)
	return e
}

func (e *serviceEnv) service(sl sealer.Sealer, mirror *opensheetsync.Mirror) *opensheetsync.Service {
	return e.serviceWith(e.store, sl, mirror)
}

func (e *serviceEnv) serviceWith(store opensheetsync.Store, sl sealer.Sealer, mirror *opensheetsync.Mirror) *opensheetsync.Service {
	log := slog.New(slog.NewTextHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return opensheetsync.NewService(store, log, apperror.NewReporter(log, false).Unexpected,
		opensheetsync.ServiceDeps{
			Mirror: mirror, Members: e.members, Sealer: sl,
			Endpoint:   opensheetsync.Endpoint{BaseURL: e.sheets.URL(), AllowPrivateHosts: true, Timeout: 2 * time.Second},
			UnitOfWork: fakes.UnitOfWork, Now: func() time.Time { return e.now },
		})
}

func settings(key string) opensheetsync.Settings {
	return opensheetsync.Settings{OSOrg: "acme", OSProject: "home", APIKey: key, Sheets: opensheetsync.DefaultSheetSlugs()}
}

func TestService_EveryChangeNeedsAnOwnerOrAdmin(t *testing.T) {
	e := newServiceEnv(t)
	addContractSheets(e.sheets, true)
	plain := uuid.New()
	e.members.SeatMember(e.tc.OrgID, plain)
	member := tenant.Into(context.Background(), tenant.Context{OrgID: e.tc.OrgID, ProjectID: e.tc.ProjectID, UserID: plain})
	key := tenant.Into(context.Background(), tenant.Context{OrgID: e.tc.OrgID, ProjectID: e.tc.ProjectID})
	for name, ctx := range map[string]context.Context{"plain member": member, "API key principal": key} {
		_, err := e.svc.Test(ctx, settings(goodKey))
		require.True(t, org.IsNotManagerError(err), "%s Test: %v", name, err)
		_, _, err = e.svc.Save(ctx, settings(goodKey))
		require.True(t, org.IsNotManagerError(err), name)
		_, err = e.svc.SetEnabled(ctx, true)
		require.True(t, org.IsNotManagerError(err), name)
		_, err = e.svc.SyncNow(ctx)
		require.True(t, org.IsNotManagerError(err), name)
		require.True(t, org.IsNotManagerError(e.svc.Delete(ctx)), name)
	}
	require.Zero(t, e.sheets.CountRequests("GET", "yasaku-transactions"), "a refused caller never reaches opensheet")
	st, err := e.svc.Status(member)
	require.NoError(t, err, "any member reads the status")
	require.Nil(t, st.Link)
}

func TestService_TestReportsEachTabAndNeverFailsOnABadSheet(t *testing.T) {
	e := newServiceEnv(t)
	e.sheets.AddSheet("yasaku-transactions", fakes.OpensheetSheet{Columns: []string{"id", "date"}, Writable: true})
	e.sheets.AddSheet("yasaku-wallets", fakes.OpensheetSheet{Columns: mustTab(t, opensheetsync.EntityWallet).Columns})

	cl, err := e.svc.Test(e.ctx, settings(goodKey))
	require.NoError(t, err)
	require.Len(t, cl, 3)
	require.True(t, opensheetsync.IsShapeMismatchError(cl[0].Err))
	require.Contains(t, cl[0].Missing, "deleted_at")
	require.True(t, opensheetsync.IsSheetNotWritableError(cl[1].Err))
	require.True(t, opensheetsync.IsSheetUnreachableError(cl[2].Err), "an unpublished sheet is the 404 mask")
	require.Equal(t, 3, e.sheets.CountRequests("GET", "yasaku-transactions")+e.sheets.CountRequests("GET", "yasaku-wallets")+e.sheets.CountRequests("GET", "yasaku-categories"))

	_, err = e.svc.Test(e.ctx, settings(""))
	require.True(t, opensheetsync.IsAPIKeyRequiredError(err), "no saved link and no key")
	bad := settings(goodKey)
	bad.OSOrg = "Acme Corp"
	_, err = e.svc.Test(e.ctx, bad)
	require.True(t, opensheetsync.IsInvalidSettingError(err))
}

func TestService_TestWithAWrongKeyIsUnreachableEverywhere(t *testing.T) {
	e := newServiceEnv(t)
	addContractSheets(e.sheets, true)
	cl, err := e.svc.Test(e.ctx, settings("osk_live_wrong_key_9999"))
	require.NoError(t, err)
	for _, c := range cl {
		require.True(t, opensheetsync.IsSheetUnreachableError(c.Err), c.Sheet)
	}
}

func TestService_SaveIsRefusedUnlessTheServerSideTestPasses(t *testing.T) {
	e := newServiceEnv(t)
	e.sheets.AddSheet("yasaku-transactions", fakes.OpensheetSheet{Columns: []string{"id"}, Writable: true})
	_, cl, err := e.svc.Save(e.ctx, settings(goodKey))
	require.Error(t, err)
	require.True(t, opensheetsync.IsShapeMismatchError(err))
	require.False(t, cl.OK())
	st, err := e.svc.Status(e.ctx)
	require.NoError(t, err)
	require.Nil(t, st.Link, "a refused Save stores nothing")

	addContractSheets(e.sheets, true)
	l, cl, err := e.svc.Save(e.ctx, settings(goodKey))
	require.NoError(t, err)
	require.True(t, cl.OK())
	require.False(t, l.Enabled, "Save never turns the mirror on")
	require.NotNil(t, l.VerifiedAt)
	require.Equal(t, "abcd", l.APIKeyHint)
	require.NotContains(t, string(l.APIKeySealed), goodKey, "the key is sealed at rest")
	require.Equal(t, e.tc.UserID, l.CreatedBy)
}

func TestService_AKeyReenteredOnSaveIsTestedBeforeItReplacesTheSavedOne(t *testing.T) {
	e := newServiceEnv(t)
	addContractSheets(e.sheets, true)
	first, _, err := e.svc.Save(e.ctx, settings(goodKey))
	require.NoError(t, err)

	_, cl, err := e.svc.Save(e.ctx, settings("osk_live_wrong_key_9999"))
	require.True(t, opensheetsync.IsSheetUnreachableError(err), "the typed key is tested, not the saved one")
	require.False(t, cl.OK())
	st, err := e.svc.Status(e.ctx)
	require.NoError(t, err)
	require.Equal(t, first.APIKeySealed, st.Link.APIKeySealed, "a refused key never replaces the saved one")

	const rotated = "osk_live_rotated_key_7777"
	e.sheets.SetToken(rotated)
	_, cl, err = e.svc.Save(e.ctx, settings(""))
	require.True(t, opensheetsync.IsSheetUnreachableError(err), "the saved key no longer works, so the Save is refused")
	require.False(t, cl.OK())
	l, _, err := e.svc.Save(e.ctx, settings(rotated))
	require.NoError(t, err)
	require.Equal(t, "7777", l.APIKeyHint)
	require.NotEqual(t, first.APIKeySealed, l.APIKeySealed)
}

func TestService_AnEmptyTabPassesWithItsColumnsDeferredAndAPopulatedOneIsCheckedStrictly(t *testing.T) {
	e := newServiceEnv(t)
	addContractSheets(e.sheets, true)
	cl, err := e.svc.Test(e.ctx, settings(goodKey))
	require.NoError(t, err)
	require.True(t, cl.OK(), "empty tabs report [deleted_at] and must not fail the Test")
	for _, c := range cl {
		require.True(t, c.ColumnsDeferred, c.Sheet)
		require.True(t, c.IDColumn, "publish's contract verdict stands in for the id column")
	}
	l, _, err := e.svc.Save(e.ctx, settings(goodKey))
	require.NoError(t, err)
	require.True(t, l.AwaitingFirstSync())
	st, err := e.svc.Status(e.ctx)
	require.NoError(t, err)
	require.True(t, st.Link.AwaitingFirstSync(), "the page and the RPC read it from the status")

	tab := mustTab(t, opensheetsync.EntityTransaction)
	short := slices.DeleteFunc(slices.Clone(tab.Columns), func(c string) bool { return c == "note" })
	e.sheets.AddSheet(tab.DefaultSlug, fakes.OpensheetSheet{Columns: short, Writable: true})
	e.sheets.SeedRow(tab.DefaultSlug, map[string]string{"id": "r1"})
	cl, err = e.svc.Test(e.ctx, settings(goodKey))
	require.NoError(t, err)
	require.True(t, opensheetsync.IsShapeMismatchError(cl[0].Err), "a populated tab is still checked strictly")
	require.Equal(t, []string{"note"}, cl[0].Missing)
	ae, ok := apperror.AsAppError(cl[0].Err)
	require.True(t, ok)
	require.Contains(t, ae.Message(), "opensheet may still have the old header", "capabilities never reads Google, so a header fixed a moment ago may not be there yet")
	require.False(t, cl[0].ColumnsDeferred)

	e.sheets.AddSheet(tab.DefaultSlug, fakes.OpensheetSheet{Columns: short, Writable: true})
	e.sheets.SetHeaderColumns(true)
	cl, err = e.svc.Test(e.ctx, settings(goodKey))
	require.NoError(t, err)
	require.Equal(t, []string{"note"}, cl[0].Missing, "once opensheet reports the header row, an empty tab is checked too")
	require.False(t, cl[0].ColumnsDeferred)
}

func TestService_SaveWithAnEmptyKeyKeepsTheSavedKey(t *testing.T) {
	e := newServiceEnv(t)
	addContractSheets(e.sheets, true)
	first, _, err := e.svc.Save(e.ctx, settings(goodKey))
	require.NoError(t, err)
	cl, err := e.svc.Test(e.ctx, settings(""))
	require.NoError(t, err)
	require.True(t, cl.OK(), "Test with an empty key uses the saved one")
	second, _, err := e.svc.Save(e.ctx, settings(""))
	require.NoError(t, err)
	require.Equal(t, first.APIKeySealed, second.APIKeySealed)
	require.Equal(t, "abcd", second.APIKeyHint)
	require.Empty(t, second.Settings().APIKey, "the saved settings never carry the key back out")
}

func TestService_AnUnreadableSavedKeyMustBeEnteredAgain(t *testing.T) {
	e := newServiceEnv(t)
	addContractSheets(e.sheets, true)
	_, _, err := e.svc.Save(e.ctx, settings(goodKey))
	require.NoError(t, err)
	_, err = e.svc.SetEnabled(e.ctx, true)
	require.NoError(t, err)
	_, err = e.svc.SetEnabled(e.ctx, false)
	require.NoError(t, err)

	rekeyed := e.service(newSealer(t), e.mirror)
	_, err = rekeyed.Test(e.ctx, settings(""))
	require.True(t, opensheetsync.IsKeyUnreadableError(err), "the encryption key changed: %v", err)
	require.True(t, sealer.IsOpenFailedError(err), "the cause stays reachable")
	ae, ok := apperror.AsAppError(err)
	require.True(t, ok)
	require.Equal(t, apperror.CodeOpensheetKeyUnreadable, ae.Code(), "the sealer's ENC code never leaks through the service")
	_, _, err = rekeyed.Save(e.ctx, settings(""))
	require.True(t, opensheetsync.IsKeyUnreadableError(err))
	_, err = rekeyed.SetEnabled(e.ctx, true)
	require.True(t, opensheetsync.IsKeyUnreadableError(err), "a link whose key cannot be opened is never turned on")
	st, err := rekeyed.Status(e.ctx)
	require.NoError(t, err)
	require.False(t, st.Link.Enabled)

	_, _, err = rekeyed.Save(e.ctx, settings(goodKey))
	require.NoError(t, err)
	l, err := rekeyed.SetEnabled(e.ctx, true)
	require.NoError(t, err)
	require.True(t, l.Enabled)
}

func TestService_EnableBackfillsAndDisableStops(t *testing.T) {
	e := newServiceEnv(t)
	addContractSheets(e.sheets, true)
	_, err := e.svc.SetEnabled(e.ctx, true)
	require.True(t, opensheetsync.IsLinkNotFoundError(err))
	_, _, err = e.svc.Save(e.ctx, settings(goodKey))
	require.NoError(t, err)
	for range 3 {
		e.store.SeedEntity(e.tc.OrgID, e.tc.ProjectID, opensheetsync.EntityWallet, uuid.New())
	}

	l, err := e.svc.SetEnabled(e.ctx, true)
	require.NoError(t, err)
	require.True(t, l.Enabled)
	require.Len(t, e.payloads(t), 1, "enabling marks everything and kicks the first page")
	require.Len(t, e.payloads(t)[0].Refs, 3)

	l, err = e.svc.SetEnabled(e.ctx, false)
	require.NoError(t, err)
	require.False(t, l.Enabled)
	_, err = e.svc.SyncNow(e.ctx)
	require.True(t, opensheetsync.IsLinkDisabledError(err))

	e.store.SeedEntity(e.tc.OrgID, e.tc.ProjectID, opensheetsync.EntityWallet, uuid.New())
	_, err = e.svc.SetEnabled(e.ctx, true)
	require.NoError(t, err)
	require.Len(t, e.payloads(t), 2, "every off-to-on backfills again")
	require.Len(t, e.payloads(t)[1].Refs, 4)
}

func TestService_EnablingAnAutoDisabledLinkNeedsANewSave(t *testing.T) {
	e := newServiceEnv(t)
	addContractSheets(e.sheets, true)
	_, _, err := e.svc.Save(e.ctx, settings(goodKey))
	require.NoError(t, err)
	_, err = e.svc.SetEnabled(e.ctx, true)
	require.NoError(t, err)
	loaded, err := e.store.LinkByProject(e.ctx, e.tc.OrgID, e.tc.ProjectID)
	require.NoError(t, err)
	for range opensheetsync.DisableAfter {
		_, err = e.store.SaveOutcome(e.ctx, e.tc.OrgID, e.tc.ProjectID, opensheetsync.Outcome{At: e.now, Err: "sheet gone", LinkUpdatedAt: loaded.UpdatedAt})
		require.NoError(t, err)
	}
	_, err = e.svc.SetEnabled(e.ctx, true)
	require.True(t, opensheetsync.IsNotVerifiedError(err))
	st, err := e.svc.Status(e.ctx)
	require.NoError(t, err)
	require.True(t, st.Link.AutoDisabled())
	saved, _, err := e.svc.Save(e.ctx, settings(""))
	require.NoError(t, err)
	require.False(t, saved.AutoDisabled(), "after a passing Save the page shows the plain off state, not run Test and Save again")
	st, err = e.svc.Status(e.ctx)
	require.NoError(t, err)
	require.False(t, st.Link.AutoDisabled(), "the cleared auto-disable is stored")
	require.False(t, st.Link.Enabled, "Save never turns the mirror on")
	require.Equal(t, "sheet gone", st.Link.LastError, "the last error stays for context")
	l, err := e.svc.SetEnabled(e.ctx, true)
	require.NoError(t, err)
	require.True(t, l.Enabled)
	require.Zero(t, l.FailureStreak)
}

func TestService_AResaveMakesAnOutcomeOfTheOldSettingsStale(t *testing.T) {
	e := newServiceEnv(t)
	addContractSheets(e.sheets, true)
	_, _, err := e.svc.Save(e.ctx, settings(goodKey))
	require.NoError(t, err)
	_, err = e.svc.SetEnabled(e.ctx, true)
	require.NoError(t, err)
	loaded, err := e.store.LinkByProject(e.ctx, e.tc.OrgID, e.tc.ProjectID)
	require.NoError(t, err)

	saved, _, err := e.svc.Save(e.ctx, settings(""))
	require.NoError(t, err)
	require.True(t, saved.UpdatedAt.After(loaded.UpdatedAt), "a re-save bumps updated_at even within one clock tick")
	for range opensheetsync.DisableAfter {
		_, err = e.store.SaveOutcome(e.ctx, e.tc.OrgID, e.tc.ProjectID, opensheetsync.Outcome{At: e.now, Err: "old sheet gone", LinkUpdatedAt: loaded.UpdatedAt})
		require.NoError(t, err)
	}
	st, err := e.svc.Status(e.ctx)
	require.NoError(t, err)
	require.True(t, st.Link.Enabled, "a job that loaded the old settings never penalises the new ones")
	require.Zero(t, st.Link.FailureStreak)
	require.Empty(t, st.Link.LastError)
}

func TestService_RetargetingAnEnabledLinkBackfills(t *testing.T) {
	e := newServiceEnv(t)
	addContractSheets(e.sheets, true)
	e.sheets.AddSheet("tx-2026", fakes.OpensheetSheet{Columns: mustTab(t, opensheetsync.EntityTransaction).Columns, Writable: true})
	_, _, err := e.svc.Save(e.ctx, settings(goodKey))
	require.NoError(t, err)
	_, err = e.svc.SetEnabled(e.ctx, true)
	require.NoError(t, err)
	e.store.SeedEntity(e.tc.OrgID, e.tc.ProjectID, opensheetsync.EntityTransaction, uuid.New())
	before := len(e.jobs.Recorded())

	_, _, err = e.svc.Save(e.ctx, settings(""))
	require.NoError(t, err)
	require.Len(t, e.jobs.Recorded(), before, "a re-save of the same sheets kicks nothing")

	moved := settings("")
	moved.Sheets.Transactions = "tx-2026"
	_, _, err = e.svc.Save(e.ctx, moved)
	require.NoError(t, err)
	require.Len(t, e.jobs.Recorded(), before+1, "new sheets start empty, so everything is pushed again")
}

func TestService_SwitchingToTheCurrentStateChangesNothing(t *testing.T) {
	e := newServiceEnv(t)
	addContractSheets(e.sheets, true)
	_, _, err := e.svc.Save(e.ctx, settings(goodKey))
	require.NoError(t, err)
	e.store.SeedEntity(e.tc.OrgID, e.tc.ProjectID, opensheetsync.EntityWallet, uuid.New())

	on, err := e.svc.SetEnabled(e.ctx, true)
	require.NoError(t, err)
	again, err := e.svc.SetEnabled(e.ctx, true)
	require.NoError(t, err)
	require.True(t, again.Enabled)
	require.Len(t, e.payloads(t), 1, "enabling an enabled link neither backfills nor kicks again")
	require.True(t, again.UpdatedAt.Equal(on.UpdatedAt), "a no-op enable keeps updated_at, so in-flight outcomes still count")

	off, err := e.svc.SetEnabled(e.ctx, false)
	require.NoError(t, err)
	again, err = e.svc.SetEnabled(e.ctx, false)
	require.NoError(t, err)
	require.False(t, again.Enabled)
	require.True(t, again.UpdatedAt.Equal(off.UpdatedAt), "a no-op disable keeps updated_at")
	require.Len(t, e.payloads(t), 1)
}

func TestService_DisableMovesUpdatedAtForward(t *testing.T) {
	e := newServiceEnv(t)
	addContractSheets(e.sheets, true)
	_, _, err := e.svc.Save(e.ctx, settings(goodKey))
	require.NoError(t, err)
	on, err := e.svc.SetEnabled(e.ctx, true)
	require.NoError(t, err)
	off, err := e.svc.SetEnabled(e.ctx, false)
	require.NoError(t, err)
	require.True(t, off.UpdatedAt.After(on.UpdatedAt), "the clock is fixed, so only the stamp moves it")
	st, err := e.svc.Status(e.ctx)
	require.NoError(t, err)
	require.True(t, st.Link.UpdatedAt.Equal(off.UpdatedAt))
}

func TestService_APassingSaveClearsTheFailureStreakAndKeepsTheLastError(t *testing.T) {
	e := newServiceEnv(t)
	addContractSheets(e.sheets, true)
	_, _, err := e.svc.Save(e.ctx, settings(goodKey))
	require.NoError(t, err)
	_, err = e.svc.SetEnabled(e.ctx, true)
	require.NoError(t, err)
	loaded, err := e.store.LinkByProject(e.ctx, e.tc.OrgID, e.tc.ProjectID)
	require.NoError(t, err)
	for range opensheetsync.DisableAfter - 1 {
		_, err = e.store.SaveOutcome(e.ctx, e.tc.OrgID, e.tc.ProjectID, opensheetsync.Outcome{At: e.now, Err: "opensheet refused a write to sheet yasaku-transactions: SHT014 unknown column note", LinkUpdatedAt: loaded.UpdatedAt})
		require.NoError(t, err)
	}

	l, _, err := e.svc.Save(e.ctx, settings(""))
	require.NoError(t, err)
	require.Zero(t, l.FailureStreak, "new verified settings start a fresh streak")
	require.Equal(t, "opensheet refused a write to sheet yasaku-transactions: SHT014 unknown column note", l.LastError, "the last sync error stays next to the Test that passed")
	st, err := e.svc.Status(e.ctx)
	require.NoError(t, err)
	require.Zero(t, st.Link.FailureStreak)
	require.Equal(t, "opensheet refused a write to sheet yasaku-transactions: SHT014 unknown column note", st.Link.LastError)
}

func TestService_AClientThatCannotBeBuiltIsReportedWithItsTenant(t *testing.T) {
	e := newServiceEnv(t)
	addContractSheets(e.sheets, true)
	log := slog.New(slog.NewTextHandler(e.logs, nil))
	svc := opensheetsync.NewService(e.store, log, apperror.NewReporter(log, false).Unexpected, opensheetsync.ServiceDeps{
		Mirror: e.mirror, Members: e.members, Sealer: e.sealer,
		Endpoint: opensheetsync.Endpoint{BaseURL: e.sheets.URL()}, UnitOfWork: fakes.UnitOfWork,
	})
	_, err := svc.Test(e.ctx, settings(goodKey))
	ae, ok := apperror.AsAppError(err)
	require.True(t, ok)
	require.Equal(t, apperror.CodeUnexpectedError, ae.Code(), "a loopback opensheet without allowPrivateHosts is a deployment fault")
	require.Contains(t, e.logs.String(), "org_id="+e.tc.OrgID.String())
	require.Contains(t, e.logs.String(), "project_id="+e.tc.ProjectID.String())
	require.NotContains(t, e.logs.String(), goodKey)
}

type uowGuardQueue struct {
	fakes.Queue
	inUnitOfWork int
}

func (q *uowGuardQueue) Submit(ctx context.Context, j queue.Job, data any) error {
	if _, inTx := db.CurrentTx(ctx); inTx {
		q.inUnitOfWork++
	}
	return q.Queue.Submit(ctx, j, data)
}

func TestService_KicksOnlyAfterTheUnitOfWork(t *testing.T) {
	e := newServiceEnv(t)
	addContractSheets(e.sheets, true)
	e.sheets.AddSheet("tx-2026", fakes.OpensheetSheet{Columns: mustTab(t, opensheetsync.EntityTransaction).Columns, Writable: true})
	jobs := &uowGuardQueue{}
	svc := e.service(e.sealer, opensheetsync.NewMirror(e.store, jobs, false, discardLog(), nil, func() time.Time { return e.now }))
	e.store.SeedEntity(e.tc.OrgID, e.tc.ProjectID, opensheetsync.EntityWallet, uuid.New())

	_, _, err := svc.Save(e.ctx, settings(goodKey))
	require.NoError(t, err)
	_, err = svc.SetEnabled(e.ctx, true)
	require.NoError(t, err)
	moved := settings("")
	moved.Sheets.Transactions = "tx-2026"
	_, _, err = svc.Save(e.ctx, moved)
	require.NoError(t, err)
	_, err = svc.SyncNow(e.ctx)
	require.NoError(t, err)

	require.Len(t, jobs.Recorded(), 3, "enable, retarget and sync now each kick once")
	require.Zero(t, jobs.inUnitOfWork, "a Submit inside a unit of work is refused by the yasaku submitter")
}

func TestService_SyncNowAndDelete(t *testing.T) {
	e := newServiceEnv(t)
	addContractSheets(e.sheets, true)
	_, _, err := e.svc.Save(e.ctx, settings(goodKey))
	require.NoError(t, err)
	_, err = e.svc.SetEnabled(e.ctx, true)
	require.NoError(t, err)
	e.store.SeedEntity(e.tc.OrgID, e.tc.ProjectID, opensheetsync.EntityCategory, uuid.New())
	n, err := e.svc.SyncNow(e.ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	st, err := e.svc.Status(e.ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, st.Pending)

	require.NoError(t, e.svc.Delete(e.ctx))
	st, err = e.svc.Status(e.ctx)
	require.NoError(t, err)
	require.Nil(t, st.Link)
	require.Zero(t, st.Pending, "removing the link drops its sync state")
	require.True(t, opensheetsync.IsLinkNotFoundError(e.svc.Delete(e.ctx)))
	_, err = e.svc.SyncNow(e.ctx)
	require.True(t, opensheetsync.IsLinkNotFoundError(err))
}

func TestService_NeverLogsTheKey(t *testing.T) {
	e := newServiceEnv(t)
	addContractSheets(e.sheets, true)
	_, err := e.svc.Test(e.ctx, settings(goodKey))
	require.NoError(t, err)
	_, _, err = e.svc.Save(e.ctx, settings("osk_live_wrong_key_9999"))
	require.Error(t, err)
	_, _, err = e.svc.Save(e.ctx, settings(goodKey))
	require.NoError(t, err)
	_, err = e.service(newSealer(t), e.mirror).Test(e.ctx, settings(""))
	require.Error(t, err)
	_, err = e.serviceWith(failingMarkAll{e.store}, e.sealer, e.mirror).SetEnabled(e.ctx, true)
	require.Error(t, err)

	require.NotEmpty(t, e.logs.String(), "the failures above are logged")
	require.NotContains(t, e.logs.String(), goodKey)
	require.NotContains(t, e.logs.String(), "osk_live_wrong_key_9999")
}

type failingMarkAll struct{ *fakes.OpensheetSync }

func (failingMarkAll) MarkAll(context.Context, uuid.UUID, uuid.UUID, time.Time) (int64, error) {
	return 0, errors.New("disk full")
}

func mustTab(t *testing.T, e opensheetsync.Entity) opensheetsync.Tab {
	t.Helper()
	tab, ok := opensheetsync.TabFor(e)
	require.True(t, ok)
	return tab
}
