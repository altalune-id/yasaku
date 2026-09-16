package period_test

import (
	"strings"
	"testing"

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
	}{
		{"payday moved later", civil.Date{Year: 2026, Month: 3, Day: 1}, civil.Date{Year: 2026, Month: 9, Day: 16}, 25, civil.Date{Year: 2026, Month: 3, Day: 24}},
		{"month starts on the first", civil.Date{Year: 2026, Month: 3, Day: 1}, civil.Date{Year: 2026, Month: 9, Day: 16}, 1, civil.Date{Year: 2026, Month: 3, Day: 31}},
		{"start already on the cycle day", civil.Date{Year: 2026, Month: 8, Day: 25}, civil.Date{Year: 2026, Month: 9, Day: 30}, 25, civil.Date{Year: 2026, Month: 9, Day: 24}},
		{"december rolls into january", civil.Date{Year: 2026, Month: 12, Day: 25}, civil.Date{Year: 2027, Month: 3, Day: 1}, 25, civil.Date{Year: 2027, Month: 1, Day: 24}},
		{"start day clamped to 28", civil.Date{Year: 2026, Month: 3, Day: 1}, civil.Date{Year: 2026, Month: 9, Day: 16}, 31, civil.Date{Year: 2026, Month: 3, Day: 27}},
		{"never later than today", civil.Date{Year: 2026, Month: 9, Day: 1}, civil.Date{Year: 2026, Month: 9, Day: 16}, 25, civil.Date{Year: 2026, Month: 9, Day: 16}},
		{"never before the start", civil.Date{Year: 2026, Month: 9, Day: 1}, civil.Date{Year: 2026, Month: 8, Day: 20}, 25, civil.Date{Year: 2026, Month: 9, Day: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, period.SuggestedEnd(tt.start, tt.today, tt.startDay))
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
