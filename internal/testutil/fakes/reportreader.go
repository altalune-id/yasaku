package fakes

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/report"
	"altalune.id/yasaku/money"
)

// ReportCall records the scope one Reader method was invoked with.
type ReportCall struct {
	Method         string
	OrgID          uuid.UUID
	ProjectID      uuid.UUID
	PeriodID       uuid.UUID
	PeriodIDs      []uuid.UUID
	Currency       money.Currency
	StartUTC       time.Time
	ArchivedBefore *time.Time
}

// ReportReader is an in-memory report.Reader returning canned data and recording every call.
type ReportReader struct {
	mu    sync.Mutex
	calls []ReportCall

	Ref       report.PeriodRef
	Summaries report.PeriodSummary
	Spend     []report.CategorySlice
	// SummaryBy and SpendBy override Summaries and Spend for one period id.
	SummaryBy  map[uuid.UUID]report.PeriodSummary
	SpendBy    map[uuid.UUID][]report.CategorySlice
	Income     []report.CategorySlice
	Points     []report.CashflowPoint
	FlowLines  []report.Flow
	Balances   []report.WalletLine
	Latest     map[uuid.UUID]time.Time
	PeriodErr  error
	SummaryErr error
	Err        error
}

// NewReportReader returns a ReportReader with zero canned data.
func NewReportReader() *ReportReader { return &ReportReader{} }

var _ report.Reader = (*ReportReader)(nil)

// Calls returns a copy of every recorded call in order.
func (f *ReportReader) Calls() []ReportCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ReportCall(nil), f.calls...)
}

// Last returns the most recent call recorded for method, and whether one exists.
func (f *ReportReader) Last(method string) (ReportCall, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.calls) - 1; i >= 0; i-- {
		if f.calls[i].Method == method {
			return f.calls[i], true
		}
	}
	return ReportCall{}, false
}

func (f *ReportReader) record(c ReportCall) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, c)
}

func (f *ReportReader) Period(_ context.Context, orgID, projectID, periodID uuid.UUID) (report.PeriodRef, error) {
	f.record(ReportCall{Method: "Period", OrgID: orgID, ProjectID: projectID, PeriodID: periodID})
	if f.PeriodErr != nil {
		return report.PeriodRef{}, f.PeriodErr
	}
	return f.Ref, nil
}

// LatestEntry returns the canned latest entry time for periodID.
func (f *ReportReader) LatestEntry(_ context.Context, orgID, projectID, periodID uuid.UUID) (time.Time, bool, error) {
	f.record(ReportCall{Method: "LatestEntry", OrgID: orgID, ProjectID: projectID, PeriodID: periodID})
	f.mu.Lock()
	defer f.mu.Unlock()
	at, ok := f.Latest[periodID]
	return at, ok, nil
}

func (f *ReportReader) Summary(_ context.Context, orgID, projectID, periodID uuid.UUID, currency money.Currency, startUTC time.Time, archivedBefore *time.Time) (report.PeriodSummary, error) {
	f.record(ReportCall{
		Method: "Summary", OrgID: orgID, ProjectID: projectID, PeriodID: periodID,
		Currency: currency, StartUTC: startUTC, ArchivedBefore: archivedBefore,
	})
	if err := firstErr(f.SummaryErr, f.Err); err != nil {
		return report.PeriodSummary{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.SummaryBy[periodID]; ok {
		return s, nil
	}
	return f.Summaries, nil
}

func (f *ReportReader) SpendByCategory(_ context.Context, orgID, projectID, periodID uuid.UUID, currency money.Currency) ([]report.CategorySlice, error) {
	f.record(ReportCall{Method: "SpendByCategory", OrgID: orgID, ProjectID: projectID, PeriodID: periodID, Currency: currency})
	if f.Err != nil {
		return nil, f.Err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.SpendBy[periodID]; ok {
		return s, nil
	}
	return f.Spend, nil
}

// SpendByCategories answers every requested period as SpendByCategory would, in one recorded call; a period with no spend has no key.
func (f *ReportReader) SpendByCategories(_ context.Context, orgID, projectID uuid.UUID, periodIDs []uuid.UUID, currency money.Currency) (map[uuid.UUID][]report.CategorySlice, error) {
	f.record(ReportCall{
		Method: "SpendByCategories", OrgID: orgID, ProjectID: projectID,
		PeriodIDs: append([]uuid.UUID(nil), periodIDs...), Currency: currency,
	})
	if f.Err != nil {
		return nil, f.Err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[uuid.UUID][]report.CategorySlice, len(periodIDs))
	for _, id := range periodIDs {
		s, ok := f.SpendBy[id]
		if !ok {
			s = f.Spend
		}
		if len(s) > 0 {
			out[id] = s
		}
	}
	return out, nil
}

func (f *ReportReader) IncomeByCategory(_ context.Context, orgID, projectID, periodID uuid.UUID, currency money.Currency) ([]report.CategorySlice, error) {
	f.record(ReportCall{Method: "IncomeByCategory", OrgID: orgID, ProjectID: projectID, PeriodID: periodID, Currency: currency})
	if f.Err != nil {
		return nil, f.Err
	}
	return f.Income, nil
}

func (f *ReportReader) Cashflow(_ context.Context, orgID, projectID uuid.UUID, periodIDs []uuid.UUID, currency money.Currency) ([]report.CashflowPoint, error) {
	f.record(ReportCall{
		Method: "Cashflow", OrgID: orgID, ProjectID: projectID,
		PeriodIDs: append([]uuid.UUID(nil), periodIDs...), Currency: currency,
	})
	if f.Err != nil {
		return nil, f.Err
	}
	return f.Points, nil
}

func (f *ReportReader) Flows(_ context.Context, orgID, projectID, periodID uuid.UUID, currency money.Currency) ([]report.Flow, error) {
	f.record(ReportCall{Method: "Flows", OrgID: orgID, ProjectID: projectID, PeriodID: periodID, Currency: currency})
	if f.Err != nil {
		return nil, f.Err
	}
	return f.FlowLines, nil
}

func (f *ReportReader) WalletBalances(_ context.Context, orgID, projectID uuid.UUID) ([]report.WalletLine, error) {
	f.record(ReportCall{Method: "WalletBalances", OrgID: orgID, ProjectID: projectID})
	if f.Err != nil {
		return nil, f.Err
	}
	return f.Balances, nil
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
