package report_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/report"
	"altalune.id/yasaku/money"
)

func TestPeriodRef_ArchivedBefore_FollowsTheProjectTimezoneAcrossDST(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	before := civil.Date{Year: 2026, Month: time.March, Day: 7}
	onDST := civil.Date{Year: 2026, Month: time.March, Day: 8}

	got := report.PeriodRef{End: &before}.ArchivedBefore(ny)
	require.NotNil(t, got)
	assert.Equal(t, time.Date(2026, time.March, 8, 5, 0, 0, 0, time.UTC), got.UTC(), "midnight EST on the night clocks spring forward")

	got = report.PeriodRef{End: &onDST}.ArchivedBefore(ny)
	require.NotNil(t, got)
	assert.Equal(t, time.Date(2026, time.March, 9, 4, 0, 0, 0, time.UTC), got.UTC(), "midnight EDT the day after")

	assert.Nil(t, report.PeriodRef{}.ArchivedBefore(ny), "a running period has no cutoff")
}

func TestTotalsOf(t *testing.T) {
	usd := money.Currency("USD")
	lines := []report.WalletLine{
		{WalletID: uuid.New(), Closing: idr(1_000_000)},
		{WalletID: uuid.New(), Closing: idr(250_000), ExcludeFromTotal: true},
		{WalletID: uuid.New(), Closing: money.New(5_000, usd)},
	}

	got := report.TotalsOf(lines, money.IDR)
	assert.Equal(t, idr(1_000_000), got.Spendable, "an excluded wallet stays out of the spendable total")
	assert.Equal(t, idr(1_250_000), got.Total, "the total keeps an excluded wallet")
	assert.True(t, got.Mixed, "a wallet in another currency is reported, never converted")

	other := report.TotalsOf(lines, usd)
	assert.Equal(t, money.New(5_000, usd), other.Spendable, "a wallet in another currency is never added")
	assert.Equal(t, money.New(5_000, usd), other.Total)

	assert.False(t, report.TotalsOf(lines[:2], money.IDR).Mixed)

	empty := report.TotalsOf(nil, money.IDR)
	assert.Equal(t, idr(0), empty.Spendable)
	assert.Equal(t, idr(0), empty.Total)
}
