package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	yasakuv1 "altalune.id/yasaku/gen/go/yasaku/v1"
	"altalune.id/yasaku/gen/go/yasaku/v1/yasakuv1connect"
	"altalune.id/yasaku/internal/boot"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/config"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/user"
	"altalune.id/yasaku/logger"
)

const (
	testIssuer  = "https://idp.test"
	testSubject = "sub-owner"
)

type yasakuHarness struct {
	t       *testing.T
	boot    *boot.Server
	server  *httptest.Server
	user    *user.User
	org     *org.Org
	project *project.Project
}

func newYasakuCfg(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{
		Mode: config.ModeCloud,
		HTTP: config.HTTPConfig{Addr: "127.0.0.1:0", BaseURL: "http://127.0.0.1"},
		DB: db.DBConfig{
			Driver:      db.DriverSQLite,
			DSN:         filepath.Join(t.TempDir(), "yasaku-api.db"),
			TablePrefix: "yasaku_",
			Schema:      "public",
			AutoMigrate: true,
		},
		Tenant: config.TenantConfig{
			SingletonOrg:            config.SingletonOrgConfig{Slug: "default", Name: "Default"},
			PersonalOrgSlugFallback: "personal",
			PersonalProjectSlug:     "default",
		},
		Log:  logger.Config{Level: "error", Format: "json"},
		Mail: config.MailConfig{Driver: "console", From: "no-reply@example.com"},
	}
}

func newYasakuHarness(t *testing.T) *yasakuHarness {
	t.Helper()
	return newYasakuHarnessOn(t, newYasakuCfg(t))
}

// newYasakuHarnessOn boots the real composition root, seeds one user/org/project and serves the
// Connect handlers, so the tests exercise production wiring rather than a copy of it.
func newYasakuHarnessOn(t *testing.T, cfg *config.Config) *yasakuHarness {
	t.Helper()
	ctx := context.Background()
	srv, err := boot.BootServer(ctx, cfg, boot.WithLogger(logger.New(cfg.Log)))
	require.NoError(t, err)
	t.Cleanup(func() { _ = srv.Close() })

	u, err := srv.Users.EnsureFromOIDC(ctx, user.Claims{
		Issuer:  testIssuer,
		Subject: testSubject,
		Email:   "owner@example.com",
		Name:    "Owner",
	})
	require.NoError(t, err)

	o, err := srv.Orgs.Create(ctx, org.CreateRequest{Slug: "acme", Name: "Acme", OwnerID: u.ID})
	require.NoError(t, err)

	orgCtx := tenant.Into(ctx, tenant.Context{OrgID: o.ID, UserID: u.ID})
	p, err := srv.Projects.Create(orgCtx, o.ID, "main", "Main")
	require.NoError(t, err)

	srv.Platform.Verifier = stubVerifier{principal: session.Principal{
		UserID:     u.ID,
		Email:      u.Email,
		IDPIssuer:  testIssuer,
		IDPSubject: testSubject,
	}}

	ts := httptest.NewServer(srv.API.Handler(""))
	t.Cleanup(ts.Close)

	return &yasakuHarness{t: t, boot: srv, server: ts, user: u, org: o, project: p}
}

func testBearer() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			req.Header().Set("Authorization", "Bearer stub-token")
			return next(ctx, req)
		}
	}
}

func (h *yasakuHarness) opts() []connect.ClientOption {
	return []connect.ClientOption{connect.WithInterceptors(testBearer())}
}

func (h *yasakuHarness) base() string { return h.server.URL + "/api" }

func (h *yasakuHarness) walletClient() yasakuv1connect.WalletServiceClient {
	return yasakuv1connect.NewWalletServiceClient(http.DefaultClient, h.base(), h.opts()...)
}

func (h *yasakuHarness) txClient() yasakuv1connect.TransactionServiceClient {
	return yasakuv1connect.NewTransactionServiceClient(http.DefaultClient, h.base(), h.opts()...)
}

func (h *yasakuHarness) periodClient() yasakuv1connect.PeriodServiceClient {
	return yasakuv1connect.NewPeriodServiceClient(http.DefaultClient, h.base(), h.opts()...)
}

func (h *yasakuHarness) reportClient() yasakuv1connect.ReportServiceClient {
	return yasakuv1connect.NewReportServiceClient(http.DefaultClient, h.base(), h.opts()...)
}

func (h *yasakuHarness) categoryClient() yasakuv1connect.CategoryServiceClient {
	return yasakuv1connect.NewCategoryServiceClient(http.DefaultClient, h.base(), h.opts()...)
}

func (h *yasakuHarness) workspaceClient() yasakuv1connect.WorkspaceServiceClient {
	return yasakuv1connect.NewWorkspaceServiceClient(http.DefaultClient, h.base(), h.opts()...)
}

// createWallet is the confirmed happy path every later fixture builds on.
func (h *yasakuHarness) createWallet(ctx context.Context, name string, opening *yasakuv1.Money) *yasakuv1.Wallet {
	h.t.Helper()
	resp, err := h.walletClient().CreateWallet(ctx, connect.NewRequest(&yasakuv1.CreateWalletRequest{
		Name:           name,
		Kind:           "bank",
		OpeningBalance: opening,
		Confirm:        true,
	}))
	require.NoError(h.t, err)
	require.Nil(h.t, resp.Msg.GetNeeds())
	require.NotNil(h.t, resp.Msg.GetResult())
	return resp.Msg.GetResult()
}

func TestCreateWallet_PreviewWritesNothing_ConfirmWrites(t *testing.T) {
	h := newYasakuHarness(t)
	ctx := t.Context()
	wc := h.walletClient()

	preview, err := wc.CreateWallet(ctx, connect.NewRequest(&yasakuv1.CreateWalletRequest{
		Name:           "BCA",
		Kind:           "bank",
		OpeningBalance: &yasakuv1.Money{Amount: "1000000"},
	}))
	require.NoError(t, err)
	require.Nil(t, preview.Msg.GetNeeds())
	require.Nil(t, preview.Msg.GetResult())
	require.NotNil(t, preview.Msg.GetPreview())
	require.Equal(t, "BCA", preview.Msg.GetPreview().GetName())
	require.Empty(t, preview.Msg.GetPreview().GetId())

	list, err := wc.ListWallets(ctx, connect.NewRequest(&yasakuv1.ListWalletsRequest{}))
	require.NoError(t, err)
	require.Empty(t, list.Msg.GetWallets(), "preview must not persist a wallet")

	confirmed, err := wc.CreateWallet(ctx, connect.NewRequest(&yasakuv1.CreateWalletRequest{
		Name:           "BCA",
		Kind:           "bank",
		OpeningBalance: &yasakuv1.Money{Amount: "1000000"},
		Confirm:        true,
	}))
	require.NoError(t, err)
	require.NotNil(t, confirmed.Msg.GetResult())
	require.NotEmpty(t, confirmed.Msg.GetResult().GetId())

	list, err = wc.ListWallets(ctx, connect.NewRequest(&yasakuv1.ListWalletsRequest{}))
	require.NoError(t, err)
	require.Len(t, list.Msg.GetWallets(), 1)

	txs, err := h.txClient().ListTransactions(ctx, connect.NewRequest(&yasakuv1.ListTransactionsRequest{}))
	require.NoError(t, err)
	require.Len(t, txs.Msg.GetTransactions(), 1, "opening balance must be recorded")
	require.Equal(t, "opening", txs.Msg.GetTransactions()[0].GetKind())
}

func TestScope_AutoSelectsSingleProject(t *testing.T) {
	h := newYasakuHarness(t)
	ctx := t.Context()

	resp, err := h.workspaceClient().Now(ctx, connect.NewRequest(&yasakuv1.NowRequest{}))
	require.NoError(t, err)
	require.NotEmpty(t, resp.Msg.GetTimezone())
	require.NotEmpty(t, resp.Msg.GetToday())
}

func TestScope_TwoProjectsNeedProject(t *testing.T) {
	h := newYasakuHarness(t)
	ctx := t.Context()

	orgCtx := tenant.Into(ctx, tenant.Context{OrgID: h.org.ID, UserID: h.user.ID})
	_, err := h.boot.Projects.Create(orgCtx, h.org.ID, "side", "Side")
	require.NoError(t, err)

	resp, err := h.walletClient().CreateWallet(ctx, connect.NewRequest(&yasakuv1.CreateWalletRequest{
		Name: "BCA",
		Kind: "bank",
	}))
	require.NoError(t, err)
	nd := resp.Msg.GetNeeds()
	require.NotNil(t, nd)
	require.Len(t, nd.GetNeeds(), 1)
	require.Equal(t, "project", nd.GetNeeds()[0].GetField())
	require.ElementsMatch(t, []string{"main", "side"}, nd.GetNeeds()[0].GetCandidates())
}

func TestScope_NonMemberOrgIsNotFound(t *testing.T) {
	h := newYasakuHarness(t)
	ctx := context.Background()

	other, err := h.boot.Users.EnsureFromOIDC(ctx, user.Claims{
		Issuer: testIssuer, Subject: "sub-other", Email: "other@example.com", Name: "Other",
	})
	require.NoError(t, err)
	foreign, err := h.boot.Orgs.Create(ctx, org.CreateRequest{Slug: "foreign", Name: "Foreign", OwnerID: other.ID})
	require.NoError(t, err)
	require.Equal(t, "foreign", foreign.Slug)

	_, err = h.walletClient().ListWallets(t.Context(), connect.NewRequest(&yasakuv1.ListWalletsRequest{
		Target: &yasakuv1.Target{Org: "foreign"},
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))

	_, err = h.walletClient().ListWallets(t.Context(), connect.NewRequest(&yasakuv1.ListWalletsRequest{
		Target: &yasakuv1.Target{Org: "no-such-org"},
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeNotFound, connect.CodeOf(err),
		"an unknown slug and a non-member slug must be indistinguishable")
}

func TestRecordExpense_AmbiguousWallet_NeedsAndRecordsNothing(t *testing.T) {
	h := newYasakuHarness(t)
	ctx := t.Context()

	h.createWallet(ctx, "BCA", nil)
	h.createWallet(ctx, "BCA Tabungan", nil)

	resp, err := h.txClient().RecordExpense(ctx, connect.NewRequest(&yasakuv1.RecordExpenseRequest{
		Wallet:  "bc",
		Amount:  &yasakuv1.Money{Amount: "50000"},
		Note:    "kopi",
		Confirm: true,
	}))
	require.NoError(t, err)
	nd := resp.Msg.GetNeeds()
	require.NotNil(t, nd)
	require.Len(t, nd.GetNeeds(), 1)
	require.Equal(t, "wallet", nd.GetNeeds()[0].GetField())
	require.ElementsMatch(t, []string{"BCA", "BCA Tabungan"}, nd.GetNeeds()[0].GetCandidates())
	require.Nil(t, resp.Msg.GetResult())

	txs, err := h.txClient().ListTransactions(ctx, connect.NewRequest(&yasakuv1.ListTransactionsRequest{}))
	require.NoError(t, err)
	require.Empty(t, txs.Msg.GetTransactions(), "confirm with unresolved needs must commit nothing")
}

func TestClosePeriod_PreviewThenConfirm_ThenReopen(t *testing.T) {
	h := newYasakuHarness(t)
	ctx := t.Context()
	w := h.createWallet(ctx, "BCA", &yasakuv1.Money{Amount: "1000000"})
	require.NotEmpty(t, w.GetId())

	_, err := h.txClient().RecordExpense(ctx, connect.NewRequest(&yasakuv1.RecordExpenseRequest{
		Wallet:  "BCA",
		Amount:  &yasakuv1.Money{Amount: "25000"},
		Note:    "kopi",
		Confirm: true,
	}))
	require.NoError(t, err)

	pc := h.periodClient()
	preview, err := pc.ClosePeriod(ctx, connect.NewRequest(&yasakuv1.ClosePeriodRequest{}))
	require.NoError(t, err)
	require.Nil(t, preview.Msg.GetNeeds())
	require.Nil(t, preview.Msg.GetResult())
	require.NotNil(t, preview.Msg.GetPreview())
	require.Equal(t, "25000", preview.Msg.GetPreview().GetExpense().GetAmount())

	cur, err := pc.GetCurrentPeriod(ctx, connect.NewRequest(&yasakuv1.GetCurrentPeriodRequest{}))
	require.NoError(t, err)
	require.Equal(t, "open", cur.Msg.GetPeriod().GetStatus(), "preview must not close the period")

	confirmed, err := pc.ClosePeriod(ctx, connect.NewRequest(&yasakuv1.ClosePeriodRequest{Confirm: true}))
	require.NoError(t, err)
	require.NotNil(t, confirmed.Msg.GetResult())
	require.Equal(t, "closed", confirmed.Msg.GetResult().GetStatus())
	closedID := confirmed.Msg.GetResult().GetId()

	reopened, err := pc.ReopenPeriod(ctx, connect.NewRequest(&yasakuv1.ReopenPeriodRequest{
		Period:  closedID,
		Confirm: true,
	}))
	require.NoError(t, err)
	require.NotNil(t, reopened.Msg.GetResult())
	require.Equal(t, "open", reopened.Msg.GetResult().GetStatus())
}

func TestPeriodReport_ReturnsNonZeroIncomeAndExpense(t *testing.T) {
	h := newYasakuHarness(t)
	ctx := t.Context()
	h.createWallet(ctx, "BCA", nil)

	_, err := h.categoryClient().SeedDefaultCategories(ctx, connect.NewRequest(&yasakuv1.SeedDefaultCategoriesRequest{
		Confirm: true,
	}))
	require.NoError(t, err)

	tc := h.txClient()
	_, err = tc.RecordIncome(ctx, connect.NewRequest(&yasakuv1.RecordIncomeRequest{
		Wallet:  "BCA",
		Amount:  &yasakuv1.Money{Amount: "5000000"},
		Note:    "gaji",
		Confirm: true,
	}))
	require.NoError(t, err)
	_, err = tc.RecordExpense(ctx, connect.NewRequest(&yasakuv1.RecordExpenseRequest{
		Wallet:  "BCA",
		Amount:  &yasakuv1.Money{Amount: "125000"},
		Note:    "makan",
		Confirm: true,
	}))
	require.NoError(t, err)

	rep, err := h.reportClient().PeriodReport(ctx, connect.NewRequest(&yasakuv1.PeriodReportRequest{}))
	require.NoError(t, err)
	require.Equal(t, "5000000", rep.Msg.GetIncome().GetAmount())
	require.Equal(t, "125000", rep.Msg.GetExpense().GetAmount())
	require.Equal(t, int32(2), rep.Msg.GetTxCount())
	require.NotEmpty(t, rep.Msg.GetWallets())
}

func TestPeriodField_RefusesANameAndOffersIDs(t *testing.T) {
	h := newYasakuHarness(t)
	ctx := t.Context()
	h.createWallet(ctx, "BCA", &yasakuv1.Money{Amount: "100000"})

	resp, err := h.periodClient().ReopenPeriod(ctx, connect.NewRequest(&yasakuv1.ReopenPeriodRequest{
		Period: "Sep 2026",
	}))
	require.NoError(t, err)
	nd := resp.Msg.GetNeeds()
	require.NotNil(t, nd)
	require.Equal(t, "period", nd.GetNeeds()[0].GetField())
	require.NotEmpty(t, nd.GetNeeds()[0].GetCandidates())
	for _, c := range nd.GetNeeds()[0].GetCandidates() {
		require.NoError(t, uuid.Validate(c), "candidates must be period ids")
	}
}

func TestAdjustBalance_NoOpWritesNothingAndReturnsNoResult(t *testing.T) {
	h := newYasakuHarness(t)
	ctx := t.Context()
	h.createWallet(ctx, "BCA", &yasakuv1.Money{Amount: "250000"})
	wc := h.walletClient()

	preview, err := wc.AdjustBalance(ctx, connect.NewRequest(&yasakuv1.AdjustBalanceRequest{
		Wallet:        "BCA",
		TargetBalance: &yasakuv1.Money{Amount: "250000"},
	}))
	require.NoError(t, err)
	require.Nil(t, preview.Msg.GetPreview())
	require.Nil(t, preview.Msg.GetResult())
	require.NotEmpty(t, preview.Msg.GetWarning())

	confirmed, err := wc.AdjustBalance(ctx, connect.NewRequest(&yasakuv1.AdjustBalanceRequest{
		Wallet:        "BCA",
		TargetBalance: &yasakuv1.Money{Amount: "250000"},
		Confirm:       true,
	}))
	require.NoError(t, err)
	require.Nil(t, confirmed.Msg.GetResult())

	txs, err := h.txClient().ListTransactions(ctx, connect.NewRequest(&yasakuv1.ListTransactionsRequest{}))
	require.NoError(t, err)
	require.Len(t, txs.Msg.GetTransactions(), 1, "only the opening transaction should exist")
}

func TestAdjustBalance_PreviewCarriesNoIDThenConfirmWrites(t *testing.T) {
	h := newYasakuHarness(t)
	ctx := t.Context()
	h.createWallet(ctx, "BCA", &yasakuv1.Money{Amount: "250000"})
	wc := h.walletClient()

	preview, err := wc.AdjustBalance(ctx, connect.NewRequest(&yasakuv1.AdjustBalanceRequest{
		Wallet:        "BCA",
		TargetBalance: &yasakuv1.Money{Amount: "200000"},
		Date:          "2026-09-16",
	}))
	require.NoError(t, err)
	require.NotNil(t, preview.Msg.GetPreview())
	require.Equal(t, "adjustment_out", preview.Msg.GetPreview().GetKind())
	require.Equal(t, "50000", preview.Msg.GetPreview().GetAmount().GetAmount())
	require.Empty(t, preview.Msg.GetPreview().GetId(), "a preview never carries an id")
	require.Nil(t, preview.Msg.GetPreview().GetCreatedAt(), "a preview never carries timestamps of its own")

	confirmed, err := wc.AdjustBalance(ctx, connect.NewRequest(&yasakuv1.AdjustBalanceRequest{
		Wallet:        "BCA",
		TargetBalance: &yasakuv1.Money{Amount: "200000"},
		Confirm:       true,
	}))
	require.NoError(t, err)
	require.NotEmpty(t, confirmed.Msg.GetResult().GetId())

	list, err := wc.ListWallets(ctx, connect.NewRequest(&yasakuv1.ListWalletsRequest{}))
	require.NoError(t, err)
	require.Equal(t, "200000", list.Msg.GetWallets()[0].GetBalance().GetAmount())
}

// The 6/24 clamp itself is asserted in TestClampCashflowPeriods; this covers the wiring only.
func TestCashflowReport_ReturnsOnePointPerPeriod(t *testing.T) {
	h := newYasakuHarness(t)
	ctx := t.Context()
	h.createWallet(ctx, "BCA", &yasakuv1.Money{Amount: "100000"})
	_, err := h.txClient().RecordIncome(ctx, connect.NewRequest(&yasakuv1.RecordIncomeRequest{
		Wallet: "BCA", Amount: &yasakuv1.Money{Amount: "250000"}, Note: "gaji", Confirm: true,
	}))
	require.NoError(t, err)

	periods, err := h.periodClient().ListPeriods(ctx, connect.NewRequest(&yasakuv1.ListPeriodsRequest{}))
	require.NoError(t, err)
	require.Len(t, periods.Msg.GetPeriods(), 1)

	resp, err := h.reportClient().CashflowReport(ctx, connect.NewRequest(&yasakuv1.CashflowReportRequest{}))
	require.NoError(t, err)
	require.Len(t, resp.Msg.GetPoints(), 1)
	require.Equal(t, periods.Msg.GetPeriods()[0].GetId(), resp.Msg.GetPoints()[0].GetPeriod().GetId())
	require.Equal(t, "250000", resp.Msg.GetPoints()[0].GetIncome().GetAmount())
}

func TestRecordExpense_PreviewCarriesNoIDAndSuggestsACategory(t *testing.T) {
	h := newYasakuHarness(t)
	ctx := t.Context()
	h.createWallet(ctx, "BCA", &yasakuv1.Money{Amount: "1000000"})
	_, err := h.categoryClient().SeedDefaultCategories(ctx, connect.NewRequest(&yasakuv1.SeedDefaultCategoriesRequest{Confirm: true}))
	require.NoError(t, err)

	tc := h.txClient()
	_, err = tc.RecordExpense(ctx, connect.NewRequest(&yasakuv1.RecordExpenseRequest{
		Wallet: "BCA", Amount: &yasakuv1.Money{Amount: "30000"},
		Category: "Food", Note: "kopi", Confirm: true,
	}))
	require.NoError(t, err)

	preview, err := tc.RecordExpense(ctx, connect.NewRequest(&yasakuv1.RecordExpenseRequest{
		Wallet: "BCA", Amount: &yasakuv1.Money{Amount: "30000"}, Note: "kopi",
	}))
	require.NoError(t, err)
	require.NotNil(t, preview.Msg.GetPreview())
	require.Empty(t, preview.Msg.GetPreview().GetId())
	require.Nil(t, preview.Msg.GetPreview().GetCreatedAt())
	require.NotEmpty(t, preview.Msg.GetPreview().GetPeriodId())
	require.Contains(t, preview.Msg.GetWarning(), "category suggested: Food")
	require.Nil(t, preview.Msg.GetPreview().GetCategory(), "a suggestion is advisory, never applied")

	// Confirming without naming the category must not silently apply the suggestion.
	confirmed, err := tc.RecordExpense(ctx, connect.NewRequest(&yasakuv1.RecordExpenseRequest{
		Wallet: "BCA", Amount: &yasakuv1.Money{Amount: "30000"}, Note: "kopi", Confirm: true,
	}))
	require.NoError(t, err)
	require.Nil(t, confirmed.Msg.GetResult().GetCategory())
}

func TestSeedDefaultCategories_PreviewCountMatchesInserted(t *testing.T) {
	h := newYasakuHarness(t)
	ctx := t.Context()
	cc := h.categoryClient()

	preview, err := cc.SeedDefaultCategories(ctx, connect.NewRequest(&yasakuv1.SeedDefaultCategoriesRequest{}))
	require.NoError(t, err)
	require.Equal(t, int32(20), preview.Msg.GetPreviewCount())

	list, err := cc.ListCategories(ctx, connect.NewRequest(&yasakuv1.ListCategoriesRequest{}))
	require.NoError(t, err)
	require.Empty(t, list.Msg.GetCategories(), "a preview must insert nothing")

	confirmed, err := cc.SeedDefaultCategories(ctx, connect.NewRequest(&yasakuv1.SeedDefaultCategoriesRequest{Confirm: true}))
	require.NoError(t, err)
	require.Equal(t, preview.Msg.GetPreviewCount(), confirmed.Msg.GetInserted())

	again, err := cc.SeedDefaultCategories(ctx, connect.NewRequest(&yasakuv1.SeedDefaultCategoriesRequest{}))
	require.NoError(t, err)
	require.Equal(t, int32(0), again.Msg.GetPreviewCount(), "seeding is idempotent")
}

func TestCreateCategory_UnacceptedIconComesBackAsNeeds(t *testing.T) {
	h := newYasakuHarness(t)
	ctx := t.Context()

	resp, err := h.categoryClient().CreateCategory(ctx, connect.NewRequest(&yasakuv1.CreateCategoryRequest{
		Name: "Kopi", Kind: "expense", Icon: "rocket", Confirm: true,
	}))
	require.NoError(t, err)
	nd := resp.Msg.GetNeeds()
	require.NotNil(t, nd)
	require.Equal(t, "icon", nd.GetNeeds()[0].GetField())
	require.Contains(t, nd.GetNeeds()[0].GetCandidates(), "utensils")

	list, err := h.categoryClient().ListCategories(ctx, connect.NewRequest(&yasakuv1.ListCategoriesRequest{}))
	require.NoError(t, err)
	require.Empty(t, list.Msg.GetCategories())
}

func TestListProjects_ReturnsEveryAddressablePair(t *testing.T) {
	h := newYasakuHarness(t)
	resp, err := h.workspaceClient().ListProjects(t.Context(), connect.NewRequest(&yasakuv1.ListProjectsRequest{}))
	require.NoError(t, err)
	require.Len(t, resp.Msg.GetProjects(), 1)
	require.Equal(t, "acme", resp.Msg.GetProjects()[0].GetOrg())
	require.Equal(t, "main", resp.Msg.GetProjects()[0].GetProject())
}

func TestRecordTransfer_PreviewThenConfirmMovesMoney(t *testing.T) {
	h := newYasakuHarness(t)
	ctx := t.Context()
	h.createWallet(ctx, "BCA", &yasakuv1.Money{Amount: "1000000"})
	h.createWallet(ctx, "Dompet", nil)
	tc := h.txClient()

	preview, err := tc.RecordTransfer(ctx, connect.NewRequest(&yasakuv1.RecordTransferRequest{
		FromWallet: "BCA", ToWallet: "Dompet", Amount: &yasakuv1.Money{Amount: "200000"},
	}))
	require.NoError(t, err)
	require.Equal(t, "BCA", preview.Msg.GetPreview().GetWallet().GetName())
	require.Equal(t, "Dompet", preview.Msg.GetPreview().GetToWallet().GetName())
	require.Empty(t, preview.Msg.GetPreview().GetId())

	_, err = tc.RecordTransfer(ctx, connect.NewRequest(&yasakuv1.RecordTransferRequest{
		FromWallet: "BCA", ToWallet: "Dompet", Amount: &yasakuv1.Money{Amount: "200000"}, Confirm: true,
	}))
	require.NoError(t, err)

	list, err := h.walletClient().ListWallets(ctx, connect.NewRequest(&yasakuv1.ListWalletsRequest{}))
	require.NoError(t, err)
	byName := map[string]string{}
	for _, w := range list.Msg.GetWallets() {
		byName[w.GetName()] = w.GetBalance().GetAmount()
	}
	require.Equal(t, "800000", byName["BCA"])
	require.Equal(t, "200000", byName["Dompet"])
}

func TestRecordBatch_PreviewWritesNothingAndReportsPerItemOutcomes(t *testing.T) {
	h := newYasakuHarness(t)
	ctx := t.Context()
	h.createWallet(ctx, "BCA", &yasakuv1.Money{Amount: "1000000"})
	tc := h.txClient()

	items := []*yasakuv1.BatchItem{
		{Kind: "expense", Wallet: "BCA", Amount: &yasakuv1.Money{Amount: "15000"}, Note: "parkir"},
		{Kind: "expense", Wallet: "no-such-wallet", Amount: &yasakuv1.Money{Amount: "15000"}},
	}
	preview, err := tc.RecordBatch(ctx, connect.NewRequest(&yasakuv1.RecordBatchRequest{Items: items}))
	require.NoError(t, err)
	require.Len(t, preview.Msg.GetPreview(), 2)
	require.NotNil(t, preview.Msg.GetPreview()[0].GetTransaction())
	require.Empty(t, preview.Msg.GetPreview()[0].GetTransaction().GetId())
	require.NotEmpty(t, preview.Msg.GetPreview()[1].GetErrorCode(), "an unresolvable item is reported, not fatal")

	before, err := tc.ListTransactions(ctx, connect.NewRequest(&yasakuv1.ListTransactionsRequest{}))
	require.NoError(t, err)
	require.Len(t, before.Msg.GetTransactions(), 1)

	confirmed, err := tc.RecordBatch(ctx, connect.NewRequest(&yasakuv1.RecordBatchRequest{Items: items, Confirm: true}))
	require.NoError(t, err)
	require.Len(t, confirmed.Msg.GetResults(), 2)
	require.NotEmpty(t, confirmed.Msg.GetResults()[0].GetTransaction().GetId())
	require.NotEmpty(t, confirmed.Msg.GetResults()[1].GetErrorCode())

	after, err := tc.ListTransactions(ctx, connect.NewRequest(&yasakuv1.ListTransactionsRequest{}))
	require.NoError(t, err)
	require.Len(t, after.Msg.GetTransactions(), 2, "only the resolvable item is recorded")
}

func TestReviseAndDeleteTransaction_PreviewsKeepTheStoredID(t *testing.T) {
	h := newYasakuHarness(t)
	ctx := t.Context()
	h.createWallet(ctx, "BCA", &yasakuv1.Money{Amount: "1000000"})
	tc := h.txClient()

	created, err := tc.RecordExpense(ctx, connect.NewRequest(&yasakuv1.RecordExpenseRequest{
		Wallet: "BCA", Amount: &yasakuv1.Money{Amount: "15000"}, Note: "parkir", Confirm: true,
	}))
	require.NoError(t, err)
	id := created.Msg.GetResult().GetId()

	note := "parkir motor"
	revisePreview, err := tc.ReviseTransaction(ctx, connect.NewRequest(&yasakuv1.ReviseTransactionRequest{
		Id: id, Note: &note,
	}))
	require.NoError(t, err)
	require.Equal(t, id, revisePreview.Msg.GetPreview().GetId(), "a revise preview keeps the stored id")
	require.Equal(t, note, revisePreview.Msg.GetPreview().GetNote())

	revised, err := tc.ReviseTransaction(ctx, connect.NewRequest(&yasakuv1.ReviseTransactionRequest{
		Id: id, Note: &note, Amount: &yasakuv1.Money{Amount: "17000"}, Confirm: true,
	}))
	require.NoError(t, err)
	require.Equal(t, "17000", revised.Msg.GetResult().GetAmount().GetAmount())

	deletePreview, err := tc.DeleteTransaction(ctx, connect.NewRequest(&yasakuv1.DeleteTransactionRequest{Id: id}))
	require.NoError(t, err)
	require.Equal(t, id, deletePreview.Msg.GetPreview().GetId())

	still, err := tc.ListTransactions(ctx, connect.NewRequest(&yasakuv1.ListTransactionsRequest{}))
	require.NoError(t, err)
	require.Len(t, still.Msg.GetTransactions(), 2, "a delete preview removes nothing")

	_, err = tc.DeleteTransaction(ctx, connect.NewRequest(&yasakuv1.DeleteTransactionRequest{Id: id, Confirm: true}))
	require.NoError(t, err)
	after, err := tc.ListTransactions(ctx, connect.NewRequest(&yasakuv1.ListTransactionsRequest{}))
	require.NoError(t, err)
	require.Len(t, after.Msg.GetTransactions(), 1)
}

func TestSearchTransactions_MatchesNoteWithinAnInclusiveRange(t *testing.T) {
	h := newYasakuHarness(t)
	ctx := t.Context()
	h.createWallet(ctx, "BCA", &yasakuv1.Money{Amount: "1000000"})
	tc := h.txClient()

	_, err := tc.RecordExpense(ctx, connect.NewRequest(&yasakuv1.RecordExpenseRequest{
		Wallet: "BCA", Amount: &yasakuv1.Money{Amount: "15000"},
		Note: "parkir motor", OccurredAt: "2026-09-10", Confirm: true,
	}))
	require.NoError(t, err)

	hit, err := tc.SearchTransactions(ctx, connect.NewRequest(&yasakuv1.SearchTransactionsRequest{
		Query: "PARKIR", From: "2026-09-10", To: "2026-09-10",
	}))
	require.NoError(t, err)
	require.Len(t, hit.Msg.GetTransactions(), 1, "both range ends are inclusive")
	require.Equal(t, "15000", hit.Msg.GetTotalOut().GetAmount())

	miss, err := tc.SearchTransactions(ctx, connect.NewRequest(&yasakuv1.SearchTransactionsRequest{
		Query: "parkir", From: "2026-09-11",
	}))
	require.NoError(t, err)
	require.Empty(t, miss.Msg.GetTransactions())
}

func TestGetSettings_AndUpdateSettings(t *testing.T) {
	h := newYasakuHarness(t)
	ctx := t.Context()
	lc := yasakuv1connect.NewLedgerServiceClient(http.DefaultClient, h.base(), h.opts()...)

	got, err := lc.GetSettings(ctx, connect.NewRequest(&yasakuv1.GetSettingsRequest{}))
	require.NoError(t, err)
	require.Equal(t, "Asia/Jakarta", got.Msg.GetSettings().GetTimezone())
	require.Equal(t, "IDR", got.Msg.GetSettings().GetCurrency())

	tz, day := "Asia/Makassar", int32(25)
	updated, err := lc.UpdateSettings(ctx, connect.NewRequest(&yasakuv1.UpdateSettingsRequest{
		Timezone: &tz, PeriodStartDay: &day,
	}))
	require.NoError(t, err)
	require.Equal(t, "Asia/Makassar", updated.Msg.GetSettings().GetTimezone())
	require.Equal(t, int32(25), updated.Msg.GetSettings().GetPeriodStartDay())
}

func TestGetWallet_AndWalletTotals(t *testing.T) {
	h := newYasakuHarness(t)
	ctx := t.Context()
	h.createWallet(ctx, "BCA", &yasakuv1.Money{Amount: "1000000"})
	wc := h.walletClient()

	got, err := wc.GetWallet(ctx, connect.NewRequest(&yasakuv1.GetWalletRequest{Wallet: "BCA"}))
	require.NoError(t, err)
	require.Equal(t, "1000000", got.Msg.GetWallet().GetBalance().GetAmount())
	require.Len(t, got.Msg.GetRecent(), 1)

	totals, err := wc.WalletTotals(ctx, connect.NewRequest(&yasakuv1.WalletTotalsRequest{}))
	require.NoError(t, err)
	require.Equal(t, "1000000", totals.Msg.GetSpendableTotal().GetAmount())
	require.NotNil(t, totals.Msg.GetPeriod())
	require.Len(t, totals.Msg.GetWallets(), 1)
}

func TestUpdateWallet_RejectedRenameLeavesTheOtherFieldsUnchanged(t *testing.T) {
	h := newYasakuHarness(t)
	ctx := t.Context()
	h.createWallet(ctx, "BCA", nil)
	wc := h.walletClient()

	blank, provider, exclude := "   ", "Bank Central Asia", true
	_, err := wc.UpdateWallet(ctx, connect.NewRequest(&yasakuv1.UpdateWalletRequest{
		Wallet:           "BCA",
		Name:             &blank,
		Provider:         &provider,
		ExcludeFromTotal: &exclude,
		Confirm:          true,
	}))
	require.Error(t, err, "a blank name must be refused")

	got, err := wc.GetWallet(ctx, connect.NewRequest(&yasakuv1.GetWalletRequest{Wallet: "BCA"}))
	require.NoError(t, err)
	require.Equal(t, "BCA", got.Msg.GetWallet().GetName())
	require.Empty(t, got.Msg.GetWallet().GetProvider(), "a refused rename must not half-commit the provider")
	require.False(t, got.Msg.GetWallet().GetExcludeFromTotal(), "a refused rename must not half-commit exclude_from_total")
}

func TestUpdateWallet_CollidingRenameLeavesTheOtherFieldsUnchanged(t *testing.T) {
	h := newYasakuHarness(t)
	ctx := t.Context()
	h.createWallet(ctx, "BCA", nil)
	h.createWallet(ctx, "Mandiri", nil)
	wc := h.walletClient()

	taken, provider := "mandiri", "Bank Central Asia"
	_, err := wc.UpdateWallet(ctx, connect.NewRequest(&yasakuv1.UpdateWalletRequest{
		Wallet: "BCA", Name: &taken, Provider: &provider, Confirm: true,
	}))
	require.Error(t, err, "a name another active wallet holds must be refused")

	got, err := wc.GetWallet(ctx, connect.NewRequest(&yasakuv1.GetWalletRequest{Wallet: "BCA"}))
	require.NoError(t, err)
	require.Equal(t, "BCA", got.Msg.GetWallet().GetName())
	require.Empty(t, got.Msg.GetWallet().GetProvider())
}

func TestUpdateWallet_AcceptedRenameAppliesEveryField(t *testing.T) {
	h := newYasakuHarness(t)
	ctx := t.Context()
	h.createWallet(ctx, "BCA", nil)
	wc := h.walletClient()

	name, provider, kind, exclude := "BCA Utama", "Bank Central Asia", "savings", true
	resp, err := wc.UpdateWallet(ctx, connect.NewRequest(&yasakuv1.UpdateWalletRequest{
		Wallet: "BCA", Name: &name, Provider: &provider, Kind: &kind, ExcludeFromTotal: &exclude,
		Confirm: true,
	}))
	require.NoError(t, err)
	require.Equal(t, "BCA Utama", resp.Msg.GetResult().GetName())
	require.Equal(t, "savings", resp.Msg.GetResult().GetKind())
	require.Equal(t, provider, resp.Msg.GetResult().GetProvider())
	require.True(t, resp.Msg.GetResult().GetExcludeFromTotal())
}

func TestResolveCategoryAnyKind_NameHeldByBothKindsIsAmbiguous(t *testing.T) {
	h := newYasakuHarness(t)
	ctx := t.Context()
	cc := h.categoryClient()

	for _, kind := range []string{"expense", "income"} {
		_, err := cc.CreateCategory(ctx, connect.NewRequest(&yasakuv1.CreateCategoryRequest{
			Name: "Bonus", Kind: kind, Confirm: true,
		}))
		require.NoError(t, err)
	}

	_, err := cc.RenameCategory(ctx, connect.NewRequest(&yasakuv1.RenameCategoryRequest{
		Category: "Bonus", Name: "Bonus Tahunan",
	}))
	require.Error(t, err, "a name held by both kinds must never silently resolve to expense")
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	require.Contains(t, err.Error(), "matches more than one category")

	list, err := cc.ListCategories(ctx, connect.NewRequest(&yasakuv1.ListCategoriesRequest{}))
	require.NoError(t, err)
	for _, c := range list.Msg.GetCategories() {
		require.Equal(t, "Bonus", c.GetName(), "nothing may have been renamed")
	}
}

func TestResolveCategoryAnyKind_AmbiguousExpenseIsNotDiscardedForASoleIncomeMatch(t *testing.T) {
	h := newYasakuHarness(t)
	ctx := t.Context()
	cc := h.categoryClient()

	seed := []struct{ name, kind string }{
		{"Kopi Pagi", "expense"},
		{"Kopi Sore", "expense"},
		{"Kopi Bonus", "income"},
	}
	for _, c := range seed {
		_, err := cc.CreateCategory(ctx, connect.NewRequest(&yasakuv1.CreateCategoryRequest{
			Name: c.name, Kind: c.kind, Confirm: true,
		}))
		require.NoError(t, err)
	}

	_, err := cc.ArchiveCategory(ctx, connect.NewRequest(&yasakuv1.ArchiveCategoryRequest{Category: "kopi"}))
	require.Error(t, err, "an ambiguous expense match must not fall through to the sole income match")
	require.Contains(t, err.Error(), "matches more than one category")
	require.Contains(t, err.Error(), "Kopi Bonus")
	require.Contains(t, err.Error(), "Kopi Pagi")
	require.Contains(t, err.Error(), "Kopi Sore")

	list, err := cc.ListCategories(ctx, connect.NewRequest(&yasakuv1.ListCategoriesRequest{}))
	require.NoError(t, err)
	require.Len(t, list.Msg.GetCategories(), 3, "nothing may have been archived")
}

func TestUnarchiveWallet_IsReachableByName(t *testing.T) {
	h := newYasakuHarness(t)
	ctx := t.Context()
	h.createWallet(ctx, "BCA", nil)
	wc := h.walletClient()

	_, err := wc.ArchiveWallet(ctx, connect.NewRequest(&yasakuv1.ArchiveWalletRequest{
		Wallet: "BCA", Confirm: true,
	}))
	require.NoError(t, err)

	restored, err := wc.UnarchiveWallet(ctx, connect.NewRequest(&yasakuv1.UnarchiveWalletRequest{
		Wallet: "BCA",
	}))
	require.NoError(t, err, "an archived wallet must be addressable by name on unarchive")
	require.False(t, restored.Msg.GetWallet().GetArchived())
}

func TestUnarchiveCategory_IsReachableByName(t *testing.T) {
	h := newYasakuHarness(t)
	ctx := t.Context()
	cc := h.categoryClient()

	_, err := cc.CreateCategory(ctx, connect.NewRequest(&yasakuv1.CreateCategoryRequest{
		Name: "Kopi", Kind: "expense", Confirm: true,
	}))
	require.NoError(t, err)
	_, err = cc.ArchiveCategory(ctx, connect.NewRequest(&yasakuv1.ArchiveCategoryRequest{Category: "Kopi"}))
	require.NoError(t, err)

	restored, err := cc.UnarchiveCategory(ctx, connect.NewRequest(&yasakuv1.UnarchiveCategoryRequest{
		Category: "Kopi",
	}))
	require.NoError(t, err, "an archived category must be addressable by name on unarchive")
	require.False(t, restored.Msg.GetCategory().GetArchived())
}

func TestReopenPeriod_EmptyPeriodAsksForAClosedOne(t *testing.T) {
	h := newYasakuHarness(t)
	ctx := t.Context()
	h.createWallet(ctx, "BCA", &yasakuv1.Money{Amount: "100000"})
	pc := h.periodClient()

	resp, err := pc.ReopenPeriod(ctx, connect.NewRequest(&yasakuv1.ReopenPeriodRequest{}))
	require.NoError(t, err)
	nd := resp.Msg.GetNeeds()
	require.NotNil(t, nd, "an empty period must not resolve to the current open one")
	require.Equal(t, "period", nd.GetNeeds()[0].GetField())
	require.Empty(t, nd.GetNeeds()[0].GetCandidates(), "no period is closed yet")

	closed, err := pc.ClosePeriod(ctx, connect.NewRequest(&yasakuv1.ClosePeriodRequest{Confirm: true}))
	require.NoError(t, err)

	resp, err = pc.ReopenPeriod(ctx, connect.NewRequest(&yasakuv1.ReopenPeriodRequest{}))
	require.NoError(t, err)
	require.Equal(t, []string{closed.Msg.GetResult().GetId()}, resp.Msg.GetNeeds().GetNeeds()[0].GetCandidates())
}
