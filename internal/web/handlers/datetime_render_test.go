package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/invite"
	"altalune.id/yasaku/internal/ledger"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/authn"
	"altalune.id/yasaku/internal/platform/capabilities"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/internal/web/handlers"
	"altalune.id/yasaku/money"
)

var utcInstant = regexp.MustCompile(`<time datetime="\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z" data-datetime`)

func TestProjectAPIKeys_RenderInstantsAsUTCTimeWithLedgerZone(t *testing.T) {
	x := newAPIKeyWebFixture(t)
	jakarta, err := time.LoadLocation("Asia/Jakarta")
	require.NoError(t, err)
	x.Deps.ProjectLocation = func(context.Context, uuid.UUID, uuid.UUID) (*time.Location, error) { return jakarta, nil }
	mux := http.NewServeMux()
	handlers.NewAPIKeyHandler(x.Deps, x.Projects, x.Keys).Register(mux)
	x.Mux = mux

	rec := x.as(t, x.owner, http.MethodPost, "/orgs/acme/projects/alpha/apikeys", url.Values{
		"name": {"ci"}, "scopes": {authn.ScopeYasakuRead}, "expires_in": {"30"},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	page := x.as(t, x.owner, http.MethodGet, "/orgs/acme/projects/alpha/apikeys", nil)
	require.Equal(t, http.StatusOK, page.Code, page.Body.String())
	body := page.Body.String()
	require.Regexp(t, utcInstant, body)
	require.Contains(t, body, `data-ledger-tz="Asia/Jakarta"`)
	require.GreaterOrEqual(t, len(utcInstant.FindAllString(body, -1)), 2, "created and expires must both render as <time>")
}

func TestInviteList_RendersExpiryAsUTCTime(t *testing.T) {
	f := newFixture(t)
	f.Deps.Caps = capabilities.Capabilities{OrgCreation: true, LocalIdentity: true, InvitesEnabled: true}
	uid := uuid.New()
	o, err := f.Orgs.Create(context.Background(), org.CreateRequest{Slug: "acme", Name: "Acme", OwnerID: uid})
	require.NoError(t, err)
	wib := time.FixedZone("WIB", 7*3600)
	inv, err := invite.New(invite.NewParams{OrgID: o.ID, Email: "a@example.com", Role: invite.RoleMember, TTL: time.Hour, Token: "tok",
		Now: time.Date(2026, 10, 2, 5, 0, 0, 0, wib)})
	require.NoError(t, err)
	inv.ExpiresAt = inv.ExpiresAt.In(wib)
	require.NoError(t, f.InvStore.Save(context.Background(), inv))

	mux := http.NewServeMux()
	handlers.NewInviteHandler(f.Deps, f.Orgs, f.Invites).Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.authedRequest(t, http.MethodGet, "/orgs/acme/invites", "", session.Principal{UserID: uid}))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `<time datetime="2026-10-01T23:00:00Z" data-datetime`)
}

func TestTransactionList_RendersTheLedgerDayWithTheRecordedMoment(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	jakarta, err := time.LoadLocation("Asia/Jakarta")
	require.NoError(t, err)
	_, err = f.Transactions.Record(f.projectCtx(t), transaction.RecordInput{
		WalletID: f.Cash, Kind: transaction.KindExpense, Amount: money.New(1000000, money.IDR), Note: "Bakso",
		OccurredAt: time.Date(2026, 10, 1, 5, 0, 0, 0, jakarta),
	})
	require.NoError(t, err)

	mux := http.NewServeMux()
	f.txHandler().Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.request(t, http.MethodGet, f.path("/transactions"), nil, false))
	require.Equal(t, http.StatusOK, rec.Code)

	body := rec.Body.String()
	require.Contains(t, body, `<time datetime="2026-10-01" data-ledger-date data-date-style="compact" data-datetime-alt="2026-09-30T22:00:00Z" data-ledger-tz="Asia/Jakarta" data-dt-alt-label="`)
	require.Contains(t, body, `">1 Oct</time>`)
	require.False(t, buttonInsideLink(body), "a popover trigger must not sit inside a link")
}

var linkOrButton = regexp.MustCompile(`<a\b|</a>|<button\b`)

func buttonInsideLink(body string) bool {
	depth := 0
	for _, tag := range linkOrButton.FindAllString(body, -1) {
		switch tag {
		case "<a":
			depth++
		case "</a>":
			depth--
		default:
			if depth > 0 {
				return true
			}
		}
	}
	return false
}

func TestTransactionEdit_ShowsWhenTheRowWasRecorded(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	tx, err := f.Transactions.Record(f.projectCtx(t), transaction.RecordInput{
		WalletID: f.Cash, Kind: transaction.KindExpense, Amount: money.New(1000000, money.IDR), Note: "Bakso",
		OccurredAt: time.Now(),
	})
	require.NoError(t, err)

	mux := http.NewServeMux()
	f.txHandler().Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, f.request(t, http.MethodGet, f.path("/transactions/"+tx.ID.String()+"/edit"), nil, false))
	require.Equal(t, http.StatusOK, rec.Code)

	body := rec.Body.String()
	require.Contains(t, body, `<time datetime="`+tx.CreatedAt.UTC().Format(time.RFC3339)+`" data-datetime data-ledger-tz="Asia/Jakarta"`)
	require.Contains(t, body, "data-tz-hint")
}

type countingLedgerStore struct {
	ledger.Store
	reads atomic.Int32
}

func (c *countingLedgerStore) ByProject(ctx context.Context, orgID, projectID uuid.UUID) (*ledger.Settings, error) {
	c.reads.Add(1)
	return c.Store.ByProject(ctx, orgID, projectID)
}

func TestProjectZone_IsResolvedOncePerRequest(t *testing.T) {
	t.Parallel()
	f := newTxFixture(t)
	tx, err := f.Transactions.Record(f.projectCtx(t), transaction.RecordInput{
		WalletID: f.Cash, Kind: transaction.KindExpense, Amount: money.New(1000000, money.IDR), Note: "Bakso",
		OccurredAt: time.Now(),
	})
	require.NoError(t, err)
	store := &countingLedgerStore{Store: fakes.NewLedger()}
	f.Ledgers = ledger.NewService(store, discardLogger(), passthroughUnexpected())
	f.Deps.ProjectLocation = f.Ledgers.Location

	mux := http.NewServeMux()
	f.txHandler().Register(mux)
	for _, target := range []string{"/transactions", "/transactions/" + tx.ID.String() + "/edit"} {
		store.reads.Store(0)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, f.request(t, http.MethodGet, f.path(target), nil, false))
		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, int32(2), store.reads.Load(), "%s must read the ledger settings once for its zone and once for its currency", target)
	}
}
