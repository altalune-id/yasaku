package blog

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func wibInstant() time.Time {
	return time.Date(2026, 10, 1, 23, 30, 0, 0, time.FixedZone("WIB", 7*3600))
}

func requireUTC(t *testing.T, got time.Time) {
	t.Helper()
	require.Same(t, time.UTC, got.Location())
	require.Equal(t, time.Date(2026, 10, 1, 16, 30, 0, 0, time.UTC), got)
}

func TestPgPostRow_ReadsInstantsInUTC(t *testing.T) {
	at := wibInstant()
	p := (&pgPostRow{CreatedAt: at, UpdatedAt: at, FirstPublishedAt: &at}).toPost()
	requireUTC(t, p.CreatedAt)
	requireUTC(t, p.UpdatedAt)
	requireUTC(t, *p.FirstPublishedAt)
}
