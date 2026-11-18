package controlplane_test

import (
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/civil"
	yasakuv1 "altalune.id/yasaku/gen/go/yasaku/v1"
	"altalune.id/yasaku/gen/go/yasaku/v1/yasakuv1connect"
	"altalune.id/yasaku/internal/apperror"
)

func (h *yasakuHarness) todayInLedger(t *testing.T) civil.Date {
	t.Helper()
	lc := yasakuv1connect.NewLedgerServiceClient(http.DefaultClient, h.base(), h.opts()...)
	st, err := lc.GetSettings(t.Context(), connect.NewRequest(&yasakuv1.GetSettingsRequest{}))
	require.NoError(t, err)
	loc, err := time.LoadLocation(st.Msg.GetSettings().GetTimezone())
	require.NoError(t, err)
	return civil.DateOf(time.Now(), loc)
}

func (h *yasakuHarness) recordTodayExpense(t *testing.T) {
	t.Helper()
	h.createWallet(t.Context(), "BCA", nil)
	_, err := h.txClient().RecordExpense(t.Context(), connect.NewRequest(&yasakuv1.RecordExpenseRequest{
		Wallet: "BCA", Amount: &yasakuv1.Money{Amount: "25000"}, Note: "kopi", Confirm: true,
	}))
	require.NoError(t, err)
}

// An empty end_date keeps an entry dated today inside the closed period, so nothing is frozen outside its range.
func TestClosePeriod_WithoutAnEndDateKeepsTodaysEntryInsideThePeriod(t *testing.T) {
	h := newYasakuHarness(t)
	h.recordTodayExpense(t)
	today := h.todayInLedger(t)

	closed, err := h.periodClient().ClosePeriod(t.Context(), connect.NewRequest(&yasakuv1.ClosePeriodRequest{Confirm: true}))
	require.NoError(t, err)
	require.Equal(t, today.String(), closed.Msg.GetResult().GetEndDate())
	require.Equal(t, int32(1), closed.Msg.GetResult().GetSnapshot().GetTxCount(), "today's expense is in the frozen totals")

	cur, err := h.periodClient().GetCurrentPeriod(t.Context(), connect.NewRequest(&yasakuv1.GetCurrentPeriodRequest{}))
	require.NoError(t, err)
	require.Equal(t, today.AddDays(1).String(), cur.Msg.GetPeriod().GetStartDate(), "the next period starts tomorrow")
}

// SECURITY: money correctness: an end date before the latest entry is refused in preview and in close, never a silent split.
func TestClosePeriod_AnEndBeforeTheLatestEntryIsPRD009(t *testing.T) {
	h := newYasakuHarness(t)
	today := h.todayInLedger(t)
	startDay := int32(today.Day%28 + 1)
	lc := yasakuv1connect.NewLedgerServiceClient(http.DefaultClient, h.base(), h.opts()...)
	_, err := lc.UpdateSettings(t.Context(), connect.NewRequest(&yasakuv1.UpdateSettingsRequest{PeriodStartDay: &startDay}))
	require.NoError(t, err, "a cycle that did not start today, so yesterday is inside the period")
	h.recordTodayExpense(t)
	yesterday := today.AddDays(-1).String()

	for _, confirm := range []bool{false, true} {
		_, err = h.periodClient().ClosePeriod(t.Context(), connect.NewRequest(&yasakuv1.ClosePeriodRequest{EndDate: yesterday, Confirm: confirm}))
		require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err), "confirm=%v: %v", confirm, err)
		require.Equal(t, apperror.CodePeriodEntriesAfterEnd, appErrorDetail(t, err).GetCode(), "confirm=%v", confirm)
	}
	cur, cErr := h.periodClient().GetCurrentPeriod(t.Context(), connect.NewRequest(&yasakuv1.GetCurrentPeriodRequest{}))
	require.NoError(t, cErr)
	require.Equal(t, "open", cur.Msg.GetPeriod().GetStatus(), "the period stays open")
}

// A future-dated entry makes the default close name it as PRD009 rather than freeze next month into this period.
func TestClosePeriod_AFutureEntryIsPRD009WithoutAnEndDate(t *testing.T) {
	h := newYasakuHarness(t)
	tomorrow := h.todayInLedger(t).AddDays(1).String()
	h.createWallet(t.Context(), "BCA", nil)
	_, err := h.txClient().RecordExpense(t.Context(), connect.NewRequest(&yasakuv1.RecordExpenseRequest{
		Wallet: "BCA", Amount: &yasakuv1.Money{Amount: "25000"}, Note: "langganan", OccurredAt: tomorrow, Confirm: true,
	}))
	require.NoError(t, err)

	for _, confirm := range []bool{false, true} {
		_, err = h.periodClient().ClosePeriod(t.Context(), connect.NewRequest(&yasakuv1.ClosePeriodRequest{Confirm: confirm}))
		require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err), "confirm=%v: %v", confirm, err)
		detail := appErrorDetail(t, err)
		require.Equal(t, apperror.CodePeriodEntriesAfterEnd, detail.GetCode(), "confirm=%v", confirm)
		require.Equal(t, tomorrow, detail.GetMeta()["latest_entry_date"], "the refusal names the future entry's date")
	}
}
