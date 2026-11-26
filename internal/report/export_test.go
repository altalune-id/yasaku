package report_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/platform/authz"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/report"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/money"
)

const bom = "\ufeff"

type exportSource struct {
	wallets    []report.ExportWallet
	categories []report.ExportCategory
	txs        []report.ExportTransaction
	periods    []report.ExportPeriod
}

func (s *exportSource) Wallets(context.Context) ([]report.ExportWallet, error) { return s.wallets, nil }
func (s *exportSource) Categories(context.Context) ([]report.ExportCategory, error) {
	return s.categories, nil
}
func (s *exportSource) Transactions(context.Context) ([]report.ExportTransaction, error) {
	return s.txs, nil
}
func (s *exportSource) Periods(context.Context) ([]report.ExportPeriod, error) { return s.periods, nil }

type exportEnv struct {
	exp   *report.Exporter
	audit *fakes.Audit
	tc    tenant.Context
	src   *exportSource
}

func newExportEnv(t *testing.T) exportEnv {
	t.Helper()
	jakarta, err := time.LoadLocation("Asia/Jakarta")
	require.NoError(t, err)
	at := time.Date(2026, 3, 31, 20, 30, 0, 0, time.UTC)
	archived := at.Add(-time.Hour)
	walletID, catID, periodID := uuid.New(), uuid.New(), uuid.New()
	end := civil.Date{Year: 2026, Month: 3, Day: 31}
	src := &exportSource{
		wallets: []report.ExportWallet{
			{ID: walletID, Name: "BCA, main", Kind: "bank", Currency: money.IDR, CreatedAt: at},
			{ID: uuid.New(), Name: "Old cash", Kind: "cash", Currency: money.Currency("USD"), ArchivedAt: &archived, CreatedAt: at},
		},
		categories: []report.ExportCategory{
			{ID: catID, Name: "Food", Kind: "expense", CreatedAt: at},
			{ID: uuid.New(), Name: "Hobby", Kind: "expense", ArchivedAt: &archived, CreatedAt: at},
		},
		txs: []report.ExportTransaction{
			{ID: uuid.New(), OccurredAt: at, Kind: "expense", Amount: money.New(1250, money.Currency("USD")), WalletID: walletID, CategoryID: &catID, PeriodID: &periodID, Note: `nasi "goreng"`},
		},
		periods: []report.ExportPeriod{
			{ID: periodID, Name: "March", Start: civil.Date{Year: 2026, Month: 3, Day: 1}, End: &end, ClosedAt: &at},
		},
	}
	rec := &fakes.Audit{}
	log := slog.New(slog.DiscardHandler)
	exp := report.NewExporter(src, &fakeSettings{currency: money.IDR, loc: jakarta}, fakes.UnitOfWork, rec, log, apperror.NewReporter(log, false).Unexpected)
	return exportEnv{exp: exp, audit: rec, src: src, tc: tenant.Context{OrgID: uuid.New(), ProjectID: uuid.New(), UserID: uuid.New()}}
}

func readZip(t *testing.T, b []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	require.NoError(t, err)
	out := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		require.NoError(t, err)
		raw, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.NoError(t, rc.Close())
		out[f.Name] = string(raw)
	}
	return out
}

func rows(t *testing.T, s string) [][]string {
	t.Helper()
	recs, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(s, bom))).ReadAll()
	require.NoError(t, err)
	return recs
}

func TestExport_ZipShapeAndBOM(t *testing.T) {
	e := newExportEnv(t)
	var buf bytes.Buffer
	require.NoError(t, e.exp.WriteZip(fakes.Owner(t.Context(), e.tc), &buf))
	files := readZip(t, buf.Bytes())
	names := make([]string, 0, len(files))
	for n, body := range files {
		names = append(names, n)
		require.True(t, strings.HasPrefix(body, bom), "%s starts with a UTF-8 BOM", n)
	}
	slices.Sort(names)
	require.Equal(t, []string{"categories.csv", "periods.csv", "transactions.csv", "wallets.csv"}, names)

	w := rows(t, files["wallets.csv"])
	require.Equal(t, []string{"id", "name", "kind", "provider", "currency", "exclude_from_total", "created_at", "archived_at"}, w[0])
	require.Len(t, w, 3, "archived wallets are exported")
	require.Equal(t, "BCA, main", w[1][1])
	require.Equal(t, "2026-04-01T03:30:00+07:00", w[1][6], "ISO 8601 in the project time zone")
	require.Empty(t, w[1][7])
	require.Equal(t, "2026-04-01T02:30:00+07:00", w[2][7])

	c := rows(t, files["categories.csv"])
	require.Equal(t, []string{"id", "name", "kind", "created_at", "archived_at"}, c[0])
	require.Len(t, c, 3, "archived categories are exported")
	require.NotEmpty(t, c[2][4])

	tx := rows(t, files["transactions.csv"])
	require.Equal(t, []string{"id", "occurred_at", "kind", "amount", "currency", "wallet_id", "to_wallet_id", "category_id", "period_id", "note"}, tx[0])
	require.Equal(t, "2026-04-01T03:30:00+07:00", tx[1][1])
	require.Equal(t, "12.50", tx[1][3], "a decimal amount")
	require.Equal(t, "USD", tx[1][4])
	require.Empty(t, tx[1][6])
	require.Equal(t, `nasi "goreng"`, tx[1][9])

	p := rows(t, files["periods.csv"])
	require.Equal(t, []string{"id", "name", "start_date", "end_date", "closed_at"}, p[0])
	require.Equal(t, []string{"2026-03-01", "2026-03-31", "2026-04-01T03:30:00+07:00"}, p[1][2:])
}

func TestExport_NeedsReadPermissions(t *testing.T) {
	all := []authz.Permission{authz.WalletsRead, authz.CategoriesRead, authz.TransactionsRead}
	for _, missing := range all {
		t.Run(string(missing), func(t *testing.T) {
			e := newExportEnv(t)
			held := slices.DeleteFunc(slices.Clone(all), func(p authz.Permission) bool { return p == missing })
			var buf bytes.Buffer
			err := e.exp.WriteZip(fakes.Allow(t.Context(), e.tc, held...), &buf)
			require.True(t, authz.IsDeniedError(err), "got %v", err)
			require.Zero(t, buf.Len(), "nothing is written")
			require.Empty(t, e.audit.Records())
		})
	}
	e := newExportEnv(t)
	var buf bytes.Buffer
	require.NoError(t, e.exp.WriteZip(fakes.Allow(t.Context(), e.tc, all...), &buf), "the three reads are enough")
}

func TestExport_Audited(t *testing.T) {
	e := newExportEnv(t)
	require.NoError(t, e.exp.WriteZip(fakes.Owner(t.Context(), e.tc), io.Discard))
	recs := e.audit.Records()
	require.Len(t, recs, 1)
	require.Equal(t, "project.exported", recs[0].Action)
	require.True(t, recs[0].InTx)
	require.Equal(t, e.tc.ProjectID, recs[0].ProjectID)
	require.Equal(t, e.tc.ProjectID.String(), recs[0].TargetID)
	require.Equal(t, map[string]string{"wallets": "2", "categories": "2", "transactions": "1", "periods": "1"}, recs[0].Detail)
}

func TestExport_TextCellsCannotRunAsFormulas(t *testing.T) {
	e := newExportEnv(t)
	e.src.wallets[0].Name, e.src.wallets[0].Provider = "+cmd", "@SUM(A1)"
	e.src.categories[0].Name = "-2+3"
	e.src.txs[0].Note = "=HYPERLINK(\"http://x\")"
	e.src.txs[0].Amount = money.New(-1250, money.Currency("USD"))
	e.src.periods[0].Name = "\tMarch"
	var buf bytes.Buffer
	require.NoError(t, e.exp.WriteZip(fakes.Owner(t.Context(), e.tc), &buf))
	files := readZip(t, buf.Bytes())

	w := rows(t, files["wallets.csv"])
	require.Equal(t, "'+cmd", w[1][1])
	require.Equal(t, "'@SUM(A1)", w[1][3])
	require.Equal(t, "'-2+3", rows(t, files["categories.csv"])[1][1])
	tx := rows(t, files["transactions.csv"])
	require.Equal(t, "'=HYPERLINK(\"http://x\")", tx[1][9])
	require.Equal(t, "-12.50", tx[1][3], "amounts are numbers, never escaped")
	require.Equal(t, "'\tMarch", rows(t, files["periods.csv"])[1][1])
	require.Equal(t, "Hobby", rows(t, files["categories.csv"])[2][1], "plain text is left alone")
}

func TestExport_SupportVisitRefused(t *testing.T) {
	e := newExportEnv(t)
	tc := e.tc
	tc.ReadOnly = true
	var buf bytes.Buffer
	err := e.exp.WriteZip(fakes.Owner(t.Context(), tc), &buf)
	require.True(t, authz.IsSupportReadOnlyError(err), "got %v", err)
	require.Zero(t, buf.Len(), "nothing is written")
	require.Empty(t, e.audit.Records())
}
