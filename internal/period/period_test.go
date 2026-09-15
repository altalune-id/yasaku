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
