package civil_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/civil"
)

func TestDateOf_UsesLocation(t *testing.T) {
	jakarta, _ := time.LoadLocation("Asia/Jakarta")
	// 2026-09-14 20:00 UTC is 2026-09-15 03:00 in Jakarta.
	got := civil.DateOf(time.Date(2026, 9, 14, 20, 0, 0, 0, time.UTC), jakarta)
	require.Equal(t, civil.Date{Year: 2026, Month: time.September, Day: 15}, got)
}

func TestParseDate_RoundTrip(t *testing.T) {
	d, err := civil.ParseDate("2026-02-28")
	require.NoError(t, err)
	require.Equal(t, "2026-02-28", d.String())
	require.Equal(t, "2026-03-01", d.AddDays(1).String())
	_, err = civil.ParseDate("2026-2-3")
	require.True(t, civil.IsParseError(err))
}

func TestCompare_And_Scan(t *testing.T) {
	a, _ := civil.ParseDate("2026-01-01")
	b, _ := civil.ParseDate("2026-01-02")
	require.Equal(t, -1, a.Compare(b))
	require.True(t, a.Before(b))
	var d civil.Date
	require.NoError(t, d.Scan(time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)))
	require.Equal(t, "2026-05-06", d.String())
	require.NoError(t, d.Scan("2026-07-08"))
	require.Equal(t, "2026-07-08", d.String())
	v, err := d.Value()
	require.NoError(t, err)
	require.Equal(t, "2026-07-08", v)
	require.True(t, civil.Date{}.IsZero())
}
