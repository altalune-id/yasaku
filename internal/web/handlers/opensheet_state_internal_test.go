package handlers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/opensheetsync"
	"altalune.id/yasaku/internal/web/templates"
)

func TestSheetState_FirstMatchWinsInPrecedenceOrder(t *testing.T) {
	at := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	before, after := at.Add(-time.Hour), at.Add(time.Hour)
	link := func(edit func(l *opensheetsync.Link)) *opensheetsync.Link {
		l := &opensheetsync.Link{Enabled: true, VerifiedAt: &before, LastSyncedAt: &after}
		edit(l)
		return l
	}
	on := func(*opensheetsync.Link) {}
	tests := []struct {
		name string
		st   opensheetsync.Status
		want templates.OpensheetState
	}{
		{"no link", opensheetsync.Status{}, "off"},
		{"auto-disabled", opensheetsync.Status{Link: link(func(l *opensheetsync.Link) { l.Enabled, l.AutoDisabledAt, l.VerifiedAt = false, &at, nil })}, "paused"},
		{"paused with failing rows stays paused", opensheetsync.Status{
			Link:    link(func(l *opensheetsync.Link) { l.Enabled, l.AutoDisabledAt, l.FailureStreak = false, &at, 3 }),
			Backlog: opensheetsync.Backlog{Pending: 4, Failing: 2},
		}, "paused"},
		{"turned off", opensheetsync.Status{Link: link(func(l *opensheetsync.Link) { l.Enabled = false })}, "off_manual"},
		{"off with given-up rows stays off", opensheetsync.Status{
			Link: link(func(l *opensheetsync.Link) { l.Enabled = false }), Backlog: opensheetsync.Backlog{GivenUp: 2},
		}, "off_manual"},
		{"link streak", opensheetsync.Status{Link: link(func(l *opensheetsync.Link) { l.FailureStreak = 1 })}, "failing"},
		{"rows retrying", opensheetsync.Status{Link: link(on), Backlog: opensheetsync.Backlog{Pending: 1, Failing: 1}}, "failing"},
		{"failing with given-up rows stays failing", opensheetsync.Status{
			Link: link(on), Backlog: opensheetsync.Backlog{Pending: 1, Failing: 1, GivenUp: 3},
		}, "failing"},
		{"given up", opensheetsync.Status{Link: link(on), Backlog: opensheetsync.Backlog{GivenUp: 1}}, "given_up"},
		{"never synced", opensheetsync.Status{Link: link(func(l *opensheetsync.Link) { l.LastSyncedAt = nil })}, "first"},
		{"first with pending rows stays first", opensheetsync.Status{
			Link: link(func(l *opensheetsync.Link) { l.LastSyncedAt = &before; l.VerifiedAt = &at }), Backlog: opensheetsync.Backlog{Pending: 21},
		}, "first"},
		{"pending", opensheetsync.Status{Link: link(on), Backlog: opensheetsync.Backlog{Pending: 3}}, "pending"},
		{"up to date", opensheetsync.Status{Link: link(on)}, "ok"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, sheetState(tt.st))
		})
	}
}

func TestSheetNextRetry_IsTheEarliestFutureRetryOfARowStillTrying(t *testing.T) {
	now := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	soon, later, past := now.Add(2*time.Minute), now.Add(8*time.Minute), now.Add(-time.Minute)
	row := func(attempts int, retry *time.Time) opensheetsync.FailedRow {
		return opensheetsync.FailedRow{RowFailure: opensheetsync.RowFailure{Attempts: attempts, RetryAfter: retry}}
	}
	st := func(streak int, rows ...opensheetsync.FailedRow) opensheetsync.Status {
		return opensheetsync.Status{Link: &opensheetsync.Link{Enabled: true, FailureStreak: streak}, Rows: rows}
	}
	assert.Equal(t, &soon, sheetNextRetry(st(0, row(2, &later), row(1, &soon), row(1, &past)), now))
	assert.Nil(t, sheetNextRetry(st(0, row(1, &past)), now), "a due row retries on the next tick")
	assert.Nil(t, sheetNextRetry(st(0, row(opensheetsync.MaxRowAttempts, &soon)), now), "a given-up row is not retried")
	assert.Nil(t, sheetNextRetry(st(1, row(1, &soon)), now), "a link streak retries on the reconciler tick, so the page says retry_soon")
	assert.Nil(t, sheetNextRetry(opensheetsync.Status{}, now))
}

func TestSheetReasonKey_MapsEveryReasonToItsLine(t *testing.T) {
	tests := map[opensheetsync.Reason]string{
		opensheetsync.ReasonKeyRejected:     "opensheet.reason.key_rejected",
		opensheetsync.ReasonSheetReadOnly:   "opensheet.reason.sheet_read_only",
		opensheetsync.ReasonSheetMissing:    "opensheet.reason.sheet_missing",
		opensheetsync.ReasonGoogleAccess:    "opensheet.reason.google_signin",
		opensheetsync.ReasonKeyUnreadable:   "opensheet.error.key_unreadable",
		opensheetsync.ReasonPrivateEndpoint: "opensheet.check.private_endpoint",
		opensheetsync.ReasonOther:           "opensheet.reason.other",
	}
	for r, want := range tests {
		assert.Equal(t, want, sheetReasonKey(r), string(r))
	}
}

func TestSheetRows_ExposeEachRefusedEntryWithItsReasonAndRetry(t *testing.T) {
	now := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	soon, past := now.Add(4*time.Minute), now.Add(-time.Minute)
	civilDay := civil.Date{Year: 2026, Month: time.October, Day: 31}
	rows := sheetRows([]opensheetsync.FailedRow{
		{RowFailure: opensheetsync.RowFailure{Ref: opensheetsync.Ref{Entity: opensheetsync.EntityTransaction}, Attempts: 2, LastError: "refused: SHT016 amount", RetryAfter: &soon}, Name: "Coffee", On: &civilDay},
		{RowFailure: opensheetsync.RowFailure{Ref: opensheetsync.Ref{Entity: opensheetsync.EntityWallet}, Attempts: 1, RetryAfter: &past}, Name: "BCA"},
		{RowFailure: opensheetsync.RowFailure{Ref: opensheetsync.Ref{Entity: opensheetsync.EntityCategory}, Attempts: opensheetsync.MaxRowAttempts, RetryAfter: &soon}},
	}, now)
	assert.Len(t, rows, 3)
	assert.Equal(t, "opensheet.tab.transaction", rows[0].EntityKey)
	assert.Equal(t, "Coffee", rows[0].Label)
	assert.Equal(t, &civilDay, rows[0].OccurredOn)
	assert.Equal(t, "refused: SHT016 amount", rows[0].LastError)
	assert.Equal(t, "opensheet.reason.row_refused", rows[0].ReasonKey)
	assert.Equal(t, &soon, rows[0].NextRetryAt)
	assert.Nil(t, rows[1].NextRetryAt, "a due row has no future time")
	assert.True(t, rows[2].GivenUp)
	assert.Nil(t, rows[2].NextRetryAt, "a given-up row is not retried")
	assert.Equal(t, "opensheet.tab.category", rows[2].EntityKey)
}
