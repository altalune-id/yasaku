package handlers_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/period"
)

var closeDialogRe = regexp.MustCompile(`(?s)<dialog[^>]*id="close-month-dialog".*?</dialog>`)

func TestPeriods_Page_ClosesInTwoTapsThroughAPreviewDialog(t *testing.T) {
	f := newPeriodFixture(t, defaultCaps())
	cur := f.seedBooks(t)

	body := f.do(t, http.MethodGet, f.path("/periods"), nil).Body.String()
	assert.Contains(t, body, `data-dialog-open="close-month-dialog"`)
	dialog := closeDialogRe.FindString(body)
	require.NotEmpty(t, dialog, "the close preview dialog must render on the periods page")
	assert.Contains(t, dialog, f.path("/periods/"+cur.ID.String()+"/close"))
	end := f.today(t)
	assert.Contains(t, dialog, `name="end_date" value="`+end.String()+`"`, "the books hold entries dated today, so the default keeps today inside the period")
	assert.Contains(t, dialog, `<time datetime="`+end.String()+`" data-ledger-date`, "the end date renders as a localized ledger date")
	assert.Contains(t, dialog, "IDR\u00a05,000")
	assert.Contains(t, dialog, "IDR\u00a02,000")
	assert.Contains(t, dialog, "+IDR\u00a03,000")
	assert.Contains(t, dialog, "btn-primary")
	assert.NotContains(t, dialog, "btn-error", "closing is reversible, so it is not a destructive button")
}

func TestPeriods_Page_ConfirmFromTheDialogClosesThePeriod(t *testing.T) {
	f := newPeriodFixture(t, defaultCaps())
	cur := f.seedBooks(t)

	body := f.do(t, http.MethodGet, f.path("/periods"), nil).Body.String()
	require.NotEmpty(t, closeDialogRe.FindString(body))

	rec := f.do(t, http.MethodPost, f.path("/periods/"+cur.ID.String()+"/close"), url.Values{"end_date": {f.today(t).String()}})
	require.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, f.path("/periods"), rec.Header().Get("Location"))
}

func TestPeriods_Page_ListsTheCurrentPeriodBesideClosedOnes(t *testing.T) {
	f := newPeriodFixture(t, defaultCaps())
	ctx := f.scoped(t)
	f.seedClosed(ctx, t, civil.Date{Year: 2026, Month: 8, Day: 1}, civil.Date{Year: 2026, Month: 8, Day: 31})
	current, err := period.New(f.Org.ID, f.Project.ID, civil.Date{Year: 2026, Month: 9, Day: 1}, "Sep 2026")
	require.NoError(t, err)
	require.NoError(t, f.PerStore.Save(ctx, current))

	body := closeDialogRe.ReplaceAllString(f.do(t, http.MethodGet, f.path("/periods"), nil).Body.String(), "")
	list := body[strings.Index(body, `id="period-list-title"`):]
	list = list[:strings.Index(list, "</ul>")]
	assert.Equal(t, 2, strings.Count(list, "<li"), "one row per period, the current one first")
	assert.Less(t, strings.Index(list, "period.current"), strings.Index(list, "period.status_closed"))
	assert.Contains(t, list, `badge badge-sm badge-outline shrink-0">period.status_open<`)
	assert.Contains(t, body, `overview.close_named`, "the close button names the period")
}
