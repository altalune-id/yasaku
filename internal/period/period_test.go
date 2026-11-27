package period_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/period"
)

func TestNew_DefaultsNameFromStart(t *testing.T) {
	p, err := period.New(uuid.New(), uuid.New(), civil.Date{Year: 2026, Month: 9, Day: 25}, "")
	require.NoError(t, err)
	assert.Equal(t, "Sep 2026", p.Name)
	assert.Equal(t, period.StatusOpen, p.Status)
	assert.True(t, p.IsCurrent())
	assert.False(t, p.IsLocked())
	assert.Nil(t, p.Snapshot)
}

func TestNew_TrimsAndKeepsGivenName(t *testing.T) {
	p, err := period.New(uuid.New(), uuid.New(), civil.Date{Year: 2026, Month: 9, Day: 25}, "  Gajian  ")
	require.NoError(t, err)
	assert.Equal(t, "Gajian", p.Name)
}

func TestNew_RejectsNameOver60Runes(t *testing.T) {
	_, err := period.New(uuid.New(), uuid.New(), civil.Date{Year: 2026, Month: 9, Day: 25}, strings.Repeat("é", 61))
	assert.True(t, period.IsInvalidNameError(err), "got %T: %v", err, err)
}

func TestNew_AcceptsNameOf60Runes(t *testing.T) {
	_, err := period.New(uuid.New(), uuid.New(), civil.Date{Year: 2026, Month: 9, Day: 25}, strings.Repeat("é", 60))
	require.NoError(t, err)
}

func TestNew_RejectsZeroStart(t *testing.T) {
	_, err := period.New(uuid.New(), uuid.New(), civil.Date{}, "")
	assert.True(t, period.IsInvalidRangeError(err), "got %T: %v", err, err)
}

func TestDefaultName(t *testing.T) {
	tests := []struct {
		start civil.Date
		want  string
	}{
		{civil.Date{Year: 2026, Month: 9, Day: 25}, "Sep 2026"},
		{civil.Date{Year: 2026, Month: 1, Day: 1}, "Jan 2026"},
		{civil.Date{Year: 2025, Month: 12, Day: 31}, "Dec 2025"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, period.DefaultName(tt.start))
		})
	}
}

func TestRename(t *testing.T) {
	p, err := period.New(uuid.New(), uuid.New(), civil.Date{Year: 2026, Month: 9, Day: 25}, "")
	require.NoError(t, err)

	require.NoError(t, p.Rename("  Bulan Gajian "))
	assert.Equal(t, "Bulan Gajian", p.Name)

	assert.True(t, period.IsInvalidNameError(p.Rename("   ")))
	assert.True(t, period.IsInvalidNameError(p.Rename(strings.Repeat("x", 61))))
	assert.Equal(t, "Bulan Gajian", p.Name, "a rejected rename must not mutate the aggregate")
}

func TestContains_IsInclusiveOfBothEnds(t *testing.T) {
	start := civil.Date{Year: 2026, Month: 8, Day: 25}
	end := civil.Date{Year: 2026, Month: 9, Day: 24}
	p, err := period.New(uuid.New(), uuid.New(), start, "")
	require.NoError(t, err)

	assert.True(t, p.Contains(start), "open period contains its start")
	assert.False(t, p.Contains(start.AddDays(-1)))
	assert.True(t, p.Contains(start.AddDays(4000)), "an open period has no upper bound")

	p.EndDate = &end
	assert.True(t, p.Contains(start))
	assert.True(t, p.Contains(end))
	assert.True(t, p.Contains(civil.Date{Year: 2026, Month: 9, Day: 1}))
	assert.False(t, p.Contains(start.AddDays(-1)))
	assert.False(t, p.Contains(end.AddDays(1)))
}

func TestIsCurrentAndIsLocked(t *testing.T) {
	p, err := period.New(uuid.New(), uuid.New(), civil.Date{Year: 2026, Month: 8, Day: 25}, "")
	require.NoError(t, err)
	assert.True(t, p.IsCurrent())
	assert.False(t, p.IsLocked())

	end := civil.Date{Year: 2026, Month: 9, Day: 24}
	p.EndDate = &end
	p.Status = period.StatusClosed
	assert.False(t, p.IsCurrent())
	assert.True(t, p.IsLocked())

	p.Status = period.StatusOpen
	assert.False(t, p.IsCurrent(), "a reopened period keeps its end date and is not current")
	assert.False(t, p.IsLocked())
}

func TestSuggestedEnd_FollowsTheConfiguredStartDay(t *testing.T) {
	tests := []struct {
		name     string
		start    civil.Date
		today    civil.Date
		startDay int
		want     civil.Date
		latest   *civil.Date
	}{
		{"payday moved later", civil.Date{Year: 2026, Month: 3, Day: 1}, civil.Date{Year: 2026, Month: 9, Day: 16}, 25, civil.Date{Year: 2026, Month: 3, Day: 24}, nil},
		{"month starts on the first", civil.Date{Year: 2026, Month: 3, Day: 1}, civil.Date{Year: 2026, Month: 9, Day: 16}, 1, civil.Date{Year: 2026, Month: 3, Day: 31}, nil},
		{"start already on the cycle day", civil.Date{Year: 2026, Month: 8, Day: 25}, civil.Date{Year: 2026, Month: 9, Day: 30}, 25, civil.Date{Year: 2026, Month: 9, Day: 24}, nil},
		{"december rolls into january", civil.Date{Year: 2026, Month: 12, Day: 25}, civil.Date{Year: 2027, Month: 3, Day: 1}, 25, civil.Date{Year: 2027, Month: 1, Day: 24}, nil},
		{"start day clamped to 28", civil.Date{Year: 2026, Month: 3, Day: 1}, civil.Date{Year: 2026, Month: 9, Day: 16}, 31, civil.Date{Year: 2026, Month: 3, Day: 27}, nil},
		{"yesterday, so today belongs to the next period", civil.Date{Year: 2026, Month: 9, Day: 1}, civil.Date{Year: 2026, Month: 9, Day: 16}, 25, civil.Date{Year: 2026, Month: 9, Day: 15}, nil},
		{"closing on the cycle's last day leaves that day open", civil.Date{Year: 2026, Month: 9, Day: 1}, civil.Date{Year: 2026, Month: 9, Day: 30}, 1, civil.Date{Year: 2026, Month: 9, Day: 29}, nil},
		{"yesterday crosses back into the previous month", civil.Date{Year: 2026, Month: 8, Day: 25}, civil.Date{Year: 2026, Month: 9, Day: 1}, 25, civil.Date{Year: 2026, Month: 8, Day: 31}, nil},
		{"the day after the cycle ends closes the whole cycle", civil.Date{Year: 2026, Month: 9, Day: 1}, civil.Date{Year: 2026, Month: 10, Day: 1}, 1, civil.Date{Year: 2026, Month: 9, Day: 30}, nil},
		{"a period that started today", civil.Date{Year: 2026, Month: 9, Day: 16}, civil.Date{Year: 2026, Month: 9, Day: 16}, 1, civil.Date{Year: 2026, Month: 9, Day: 16}, nil},
		{"never before the start", civil.Date{Year: 2026, Month: 9, Day: 1}, civil.Date{Year: 2026, Month: 8, Day: 20}, 25, civil.Date{Year: 2026, Month: 9, Day: 1}, nil},
		{"an entry dated today moves the end to today", civil.Date{Year: 2026, Month: 9, Day: 1}, civil.Date{Year: 2026, Month: 9, Day: 16}, 1, civil.Date{Year: 2026, Month: 9, Day: 16}, &civil.Date{Year: 2026, Month: 9, Day: 16}},
		{"an entry after the cycle end moves the end to it", civil.Date{Year: 2026, Month: 9, Day: 1}, civil.Date{Year: 2026, Month: 10, Day: 3}, 1, civil.Date{Year: 2026, Month: 10, Day: 2}, &civil.Date{Year: 2026, Month: 10, Day: 2}},
		{"a future entry never pushes the default past today", civil.Date{Year: 2026, Month: 9, Day: 1}, civil.Date{Year: 2026, Month: 9, Day: 16}, 1, civil.Date{Year: 2026, Month: 9, Day: 16}, &civil.Date{Year: 2026, Month: 9, Day: 17}},
		{"an earlier entry leaves yesterday", civil.Date{Year: 2026, Month: 9, Day: 1}, civil.Date{Year: 2026, Month: 9, Day: 16}, 1, civil.Date{Year: 2026, Month: 9, Day: 15}, &civil.Date{Year: 2026, Month: 9, Day: 3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, period.SuggestedEnd(tt.start, tt.today, tt.latest, tt.startDay))
		})
	}
}

func TestCycleEnd(t *testing.T) {
	assert.Equal(t, civil.Date{Year: 2026, Month: 9, Day: 30}, period.CycleEnd(civil.Date{Year: 2026, Month: 9, Day: 1}, 1))
	assert.Equal(t, civil.Date{Year: 2026, Month: 10, Day: 24}, period.CycleEnd(civil.Date{Year: 2026, Month: 9, Day: 25}, 25))
}

func TestCycleEnd_EarlyPayBelongsToTheComingCycle(t *testing.T) {
	d := func(y int, m time.Month, day int) civil.Date { return civil.Date{Year: y, Month: m, Day: day} }
	tests := []struct {
		name     string
		start    civil.Date
		startDay int
		want     civil.Date
	}{
		{"paid one day early", d(2026, 11, 27), 28, d(2026, 12, 27)},
		{"paid a week early", d(2026, 11, 21), 28, d(2026, 12, 27)},
		{"a close eight days before payday keeps the short cycle", d(2026, 11, 20), 28, d(2026, 11, 27)},
		{"paid on payday", d(2026, 11, 28), 28, d(2026, 12, 27)},
		{"paid late", d(2026, 11, 30), 28, d(2026, 12, 27)},
		{"early pay across the year end", d(2026, 12, 25), 28, d(2027, 1, 27)},
		{"early pay for a first-of-month payday in february", d(2026, 2, 26), 1, d(2026, 3, 31)},
		{"early pay for a january first payday", d(2026, 12, 29), 1, d(2027, 1, 31)},
		{"paid on the last day of january", d(2027, 1, 31), 1, d(2027, 2, 28)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, period.CycleEnd(tt.start, tt.startDay))
		})
	}
}

func TestLatestClosed(t *testing.T) {
	current := mustPeriod(t, civil.Date{Year: 2026, Month: 9, Day: 25})
	older := closedAt(t, civil.Date{Year: 2026, Month: 7, Day: 25}, civil.Date{Year: 2026, Month: 8, Day: 24})
	newer := closedAt(t, civil.Date{Year: 2026, Month: 8, Day: 25}, civil.Date{Year: 2026, Month: 9, Day: 24})

	assert.Nil(t, period.LatestClosed(nil))
	assert.Nil(t, period.LatestClosed([]*period.Period{current}))

	got := period.LatestClosed([]*period.Period{current, older, newer})
	require.NotNil(t, got)
	assert.Equal(t, newer.ID, got.ID)

	newer.Status = period.StatusOpen
	assert.Nil(t, period.LatestClosed([]*period.Period{current, older, newer}),
		"an already reopened period blocks every reopen")
}

func TestNextOpenStart(t *testing.T) {
	today := civil.Date{Year: 2026, Month: 10, Day: 2}
	closed := closedAt(t, civil.Date{Year: 2026, Month: 10, Day: 1}, today)
	next := mustPeriod(t, civil.Date{Year: 2026, Month: 10, Day: 3})
	later := mustPeriod(t, civil.Date{Year: 2026, Month: 11, Day: 1})
	earlier := mustPeriod(t, civil.Date{Year: 2026, Month: 9, Day: 1})

	assert.Equal(t, next.StartDate, period.NextOpenStart([]*period.Period{later, closed, next, earlier}, closed, today),
		"the earliest open period that begins after today")

	lockedNext := closedAt(t, next.StartDate, civil.Date{Year: 2026, Month: 10, Day: 31})
	assert.Equal(t, later.StartDate, period.NextOpenStart([]*period.Period{closed, lockedNext, later}, closed, today),
		"a closed period is never a target")

	assert.Equal(t, civil.Date{Year: 2026, Month: 10, Day: 3}, period.NextOpenStart([]*period.Period{closed, earlier}, closed, today),
		"with no open period ahead, the day after the closed one ends, where Close opens the next")
}

func TestParseEnd_ReportsATypedRefusal(t *testing.T) {
	got, err := period.ParseEnd(" 2026-09-24 ")
	require.NoError(t, err)
	assert.Equal(t, civil.Date{Year: 2026, Month: 9, Day: 24}, got)

	_, err = period.ParseEnd("kemarin")
	assert.True(t, period.IsInvalidRangeError(err), "got %T: %v", err, err)
}

func mustPeriod(t *testing.T, start civil.Date) *period.Period {
	t.Helper()
	p, err := period.New(uuid.New(), uuid.New(), start, "")
	require.NoError(t, err)
	return p
}

func closedAt(t *testing.T, start, end civil.Date) *period.Period {
	t.Helper()
	p := mustPeriod(t, start)
	endCopy := end
	p.EndDate = &endCopy
	p.Status = period.StatusClosed
	return p
}

func TestSharesMonth(t *testing.T) {
	d := func(m time.Month, day int) civil.Date { return civil.Date{Year: 2026, Month: m, Day: day} }
	renamed := func(p *period.Period) *period.Period { p.Name = "Holiday"; return p }
	novFirst := closedAt(t, d(11, 1), d(11, 17))
	novMid := mustPeriod(t, d(11, 18))
	octPayday := closedAt(t, d(10, 25), d(11, 24))
	novPayday := mustPeriod(t, d(11, 25))

	tests := []struct {
		name          string
		p, prev, next *period.Period
		want          bool
	}{
		{"closed half of a mid-month close", novFirst, nil, novMid, true},
		{"open half of a mid-month close", novMid, novFirst, nil, true},
		{"payday cycles keep their month", novPayday, octPayday, nil, false},
		{"no neighbors", novMid, nil, nil, false},
		{"a renamed period keeps its name", renamed(mustPeriod(t, d(11, 18))), novFirst, nil, false},
		{"a renamed neighbor frees the month", novMid, renamed(closedAt(t, d(11, 1), d(11, 17))), nil, false},
		{"same month in another year", mustPeriod(t, civil.Date{Year: 2027, Month: 11, Day: 1}), novFirst, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, period.SharesMonth(tt.p, tt.prev, tt.next))
		})
	}
}
