package controlplane_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	yasakuv1 "altalune.id/yasaku/gen/go/yasaku/v1"
	"altalune.id/yasaku/gen/go/yasaku/v1/yasakuv1connect"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/web"
)

func (h *yasakuHarness) console(method, path, body string, htmx bool) *httptest.ResponseRecorder {
	h.t.Helper()
	if h.boot.CompleteOnboarding != nil {
		h.boot.CompleteOnboarding(context.Background())
	}
	sid, err := web.NewSID()
	require.NoError(h.t, err)
	p := session.Principal{UserID: h.user.ID, Email: h.user.Email, ActiveOrgID: h.org.ID, ActiveProjectID: h.project.ID, IssuedAt: time.Now().UTC()}
	require.NoError(h.t, h.boot.Platform.Sessions.Save(context.Background(), sid, p, time.Now().Add(time.Hour)))
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, "/orgs/"+h.org.Slug+"/projects/"+h.project.Slug+path, nil)
	} else {
		r = httptest.NewRequest(method, "/orgs/"+h.org.Slug+"/projects/"+h.project.Slug+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if htmx {
		r.Header.Set("HX-Request", "true")
	}
	r.AddCookie(&http.Cookie{Name: web.SessionCookieName, Value: web.SignCookie(web.DeriveKey(h.boot.Platform.StateSecret, web.PurposeSession), sid)})
	rec := httptest.NewRecorder()
	h.boot.Web.ServeHTTP(rec, r)
	return rec
}

// The booted console, not a hand-built handler: an end before the latest entry shows the PRD009 line on the htmx preview and on submit.
func TestConsoleClose_AnEndBeforeTheLatestEntryShowsTheErrorOnTheRealStack(t *testing.T) {
	h := newYasakuHarness(t)
	ctx := t.Context()
	tz := "Asia/Jakarta"
	lc := yasakuv1connect.NewLedgerServiceClient(http.DefaultClient, h.base(), h.opts()...)
	_, err := lc.UpdateSettings(ctx, connect.NewRequest(&yasakuv1.UpdateSettingsRequest{Timezone: &tz}))
	require.NoError(t, err)
	today := h.todayInLedger(t)
	startDay := int32(today.Day%28 + 1)
	_, err = lc.UpdateSettings(ctx, connect.NewRequest(&yasakuv1.UpdateSettingsRequest{PeriodStartDay: &startDay}))
	require.NoError(t, err)
	h.recordTodayExpense(t)
	cur, err := h.periodClient().GetCurrentPeriod(ctx, connect.NewRequest(&yasakuv1.GetCurrentPeriodRequest{}))
	require.NoError(t, err)
	early := today.AddDays(-1).String()
	closePath := "/periods/" + cur.Msg.GetPeriod().GetId() + "/close"

	frag := h.console(http.MethodGet, closePath+"?end_date="+early, "", true)
	require.Equal(t, http.StatusOK, frag.Code)
	assert.Contains(t, frag.Body.String(), `id="close-end-error"`, "the htmx preview names the latest entry")

	post := h.console(http.MethodPost, closePath, "end_date="+early, false)
	require.Equal(t, http.StatusUnprocessableEntity, post.Code)
	assert.Contains(t, post.Body.String(), `id="close-end-error"`, "the refused submit names the latest entry")
	assert.Contains(t, post.Body.String(), `aria-invalid="true"`)
}
