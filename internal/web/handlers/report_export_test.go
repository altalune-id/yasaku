package handlers_test

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/report"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/web/handlers"
	"altalune.id/yasaku/money"
)

type exportRows struct{}

func (exportRows) Wallets(context.Context) ([]report.ExportWallet, error) { return nil, nil }
func (exportRows) Categories(context.Context) ([]report.ExportCategory, error) {
	return nil, nil
}
func (exportRows) Transactions(context.Context) ([]report.ExportTransaction, error) {
	return nil, nil
}
func (exportRows) Periods(context.Context) ([]report.ExportPeriod, error) { return nil, nil }

type exportZone struct{}

func (exportZone) DefaultCurrency(context.Context, uuid.UUID, uuid.UUID) (money.Currency, error) {
	return money.IDR, nil
}
func (exportZone) Location(context.Context, uuid.UUID, uuid.UUID) (*time.Location, error) {
	return time.UTC, nil
}

func TestExport_RouteServesTheZip(t *testing.T) {
	t.Parallel()
	l := newLedgerFixture(t)
	rec := &fakes.Audit{}
	h := handlers.NewReportHandler(l.f.Deps, l.f.Projects, nil, nil)
	h.Export = report.NewExporter(exportRows{}, exportZone{}, fakes.UnitOfWork, rec, discardLogger(), passthroughUnexpected())
	mux := http.NewServeMux()
	h.Register(mux)

	for _, uid := range []uuid.UUID{l.owner, l.member} {
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, l.f.authedRequest(t, http.MethodGet, "/orgs/acme/projects/alpha/export.zip", "", session.Principal{UserID: uid, ActiveOrgID: l.o.ID}))
		require.Equal(t, http.StatusOK, res.Code, res.Body.String())
		assert.Equal(t, "application/zip", res.Header().Get("Content-Type"))
		assert.Contains(t, res.Header().Get("Content-Disposition"), `attachment; filename="alpha-`)
		_, err := zip.NewReader(bytes.NewReader(res.Body.Bytes()), int64(res.Body.Len()))
		require.NoError(t, err)
	}
	assert.Len(t, rec.Records(), 2)
}
