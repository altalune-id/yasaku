package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/period"
	"altalune.id/yasaku/internal/web"
	"altalune.id/yasaku/internal/web/templates"
)

func TestLabelsOf_AMidMonthCloseNeverRepeatsALabel(t *testing.T) {
	mk := func(start civil.Date, end *civil.Date) *period.Period {
		p, err := period.New(uuid.New(), uuid.New(), start, "")
		require.NoError(t, err)
		p.EndDate = end
		return p
	}
	day := func(m time.Month, d int) civil.Date { return civil.Date{Year: 2026, Month: m, Day: d} }
	octEnd, novEnd := day(10, 31), day(11, 17)
	oct := mk(day(10, 1), &octEnd)
	novClosed := mk(day(11, 1), &novEnd)
	novOpen := mk(day(11, 18), nil)

	labels := labelsOf([]*period.Period{novOpen, novClosed, oct})
	d := web.LayoutData{Translator: i18n.NewEmbeddedBundle(i18n.EnUS).For(i18n.EnUS)}
	texts := map[string]bool{}
	for _, p := range []*period.Period{novOpen, novClosed, oct} {
		texts[templates.PeriodLabelText(d, labels[p.ID])] = true
	}
	assert.Len(t, texts, 3, "every period gets its own label: %v", texts)
	assert.False(t, labels[oct.ID].Shared, "a whole month keeps its month label")
	assert.True(t, labels[novClosed.ID].Shared)
	assert.True(t, labels[novOpen.ID].Shared)
}

func TestFirstOpenDate_FallsBackToTheDayAfterTheClosedPeriod(t *testing.T) {
	start, end := civil.Date{Year: 2026, Month: 11, Day: 1}, civil.Date{Year: 2026, Month: 11, Day: 17}
	closed, err := period.New(uuid.New(), uuid.New(), start, "")
	require.NoError(t, err)
	closed.EndDate = &end
	open, err := period.New(uuid.New(), uuid.New(), civil.Date{Year: 2026, Month: 11, Day: 20}, "")
	require.NoError(t, err)

	got, ok := firstOpenDate(open, closed)
	require.True(t, ok)
	assert.Equal(t, open.StartDate, got, "the open period's start wins")

	got, ok = firstOpenDate(nil, closed)
	require.True(t, ok)
	assert.Equal(t, end.AddDays(1), got, "with no open period, the day after the closed one")

	closed.EndDate = nil
	_, ok = firstOpenDate(nil, closed)
	assert.False(t, ok)
}

func TestEndBeforeLatest_NamesBothDates(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = r.WithContext(i18n.TranslatorInto(r.Context(), i18n.NewEmbeddedBundle(i18n.EnUS).For(i18n.EnUS)))
	end, latest := civil.Date{Year: 2026, Month: 11, Day: 17}, civil.Date{Year: 2026, Month: 11, Day: 18}
	got := endBeforeLatest(r, &period.EntriesAfterEndError{End: end, Latest: latest}, latest)
	d := web.LayoutData{Translator: i18n.TranslatorFrom(r.Context())}
	assert.Contains(t, got, "go up to "+templates.LedgerDateText(d, latest))
	assert.Contains(t, got, "end on "+templates.LedgerDateText(d, end))
}
