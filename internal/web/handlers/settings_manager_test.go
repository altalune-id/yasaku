package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/ledger"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/web/handlers"
)

const ledgerForm = "timezone=UTC&currency=USD&period_start_day=15"

type ledgerFixture struct {
	f      *handlerFixture
	mux    *http.ServeMux
	owner  uuid.UUID
	member uuid.UUID
	o      *org.Org
	ledger *ledger.Service
}

func newLedgerFixture(t *testing.T) ledgerFixture {
	t.Helper()
	f := newFixture(t)
	owner := uuid.New()
	o := f.seedOrg(t, "acme", owner)
	_, err := f.Projects.Create(setTenant(t.Context(), o.ID, owner), o.ID, "alpha", "Alpha")
	require.NoError(t, err)
	member := uuid.New()
	_, err = f.Orgs.AddMember(setTenant(t.Context(), o.ID, owner), o.ID, member, org.RoleMember)
	require.NoError(t, err)
	ledgers := ledger.NewService(fakes.NewLedger(), discardLogger(), passthroughUnexpected(), f.Orgs)
	mux := http.NewServeMux()
	handlers.NewSettingsHandler(f.Deps, f.Projects, ledgers, nil).Register(mux)
	return ledgerFixture{f: f, mux: mux, owner: owner, member: member, o: o, ledger: ledgers}
}

func (l ledgerFixture) do(t *testing.T, method string, uid uuid.UUID, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	l.mux.ServeHTTP(rec, l.f.authedRequest(t, method, "/orgs/acme/projects/alpha/settings", body, session.Principal{UserID: uid, ActiveOrgID: l.o.ID}))
	return rec
}

func (l ledgerFixture) stored(t *testing.T) *ledger.Settings {
	t.Helper()
	p, err := l.f.Projects.BySlug(setTenant(t.Context(), l.o.ID, l.owner), l.o.ID, "alpha")
	require.NoError(t, err)
	st, err := l.ledger.Get(setTenantProject(t.Context(), l.o.ID, p.ID, l.owner))
	require.NoError(t, err)
	return st
}

func TestSettings_MemberSeesLedgerReadOnly(t *testing.T) {
	t.Parallel()
	l := newLedgerFixture(t)

	member := l.do(t, http.MethodGet, l.member, "")
	require.Equal(t, http.StatusOK, member.Code)
	body := member.Body.String()
	assert.Equal(t, 3, strings.Count(body, "disabled"), "currency, time zone and month start are disabled")
	assert.NotContains(t, body, `action="/orgs/acme/projects/alpha/settings"`+" data-dirty-form data-dirty-start")
	assert.NotContains(t, body, "settings.save", "no Save for a member")

	manager := l.do(t, http.MethodGet, l.owner, "").Body.String()
	assert.Zero(t, strings.Count(manager, `name="currency" disabled`))
	assert.Contains(t, manager, `data-dirty-submit`)
}

func TestSettings_MemberPostIsForbiddenAndChangesNothing(t *testing.T) {
	t.Parallel()
	l := newLedgerFixture(t)
	before := l.stored(t)

	rec := l.do(t, http.MethodPost, l.member, ledgerForm)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, before, l.stored(t))
}

func TestSettings_ManagerPostSaves(t *testing.T) {
	t.Parallel()
	l := newLedgerFixture(t)

	rec := l.do(t, http.MethodPost, l.owner, ledgerForm)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	st := l.stored(t)
	assert.Equal(t, "UTC", st.Timezone)
	assert.EqualValues(t, "USD", st.Currency)
	assert.Equal(t, 15, st.PeriodStartDay)
}

// SECURITY: the role gate runs before the form is read, so a member is refused even a post the form would reject.
func TestSettings_MemberPostWithABadFormIsStillForbidden(t *testing.T) {
	t.Parallel()
	l := newLedgerFixture(t)
	before := l.stored(t)

	for _, body := range []string{"timezone=UTC&currency=USD&period_start_day=soon", "timezone=Not/AZone&currency=XYZ&period_start_day=15"} {
		rec := l.do(t, http.MethodPost, l.member, body)
		assert.Equal(t, http.StatusForbidden, rec.Code, "body=%s", body)
	}
	assert.Equal(t, before, l.stored(t))
}
