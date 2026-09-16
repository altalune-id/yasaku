package api

import (
	"context"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	yasakuv1 "altalune.id/yasaku/gen/go/yasaku/v1"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/period"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/report"
)

const (
	defaultCashflowPeriods = 6
	maxCashflowPeriods     = 24
)

// ReportService implements yasaku.v1.ReportService.
type ReportService struct {
	scope   scopeResolver
	reports *report.Service
	periods *period.Service
}

// NewReportService binds the handler to its collaborators.
func NewReportService(orgs *org.Service, projects *project.Service, reports *report.Service, periods *period.Service) *ReportService {
	return &ReportService{
		scope:   scopeResolver{orgs: orgs, projects: projects},
		reports: reports,
		periods: periods,
	}
}

// PeriodReport totals one period and breaks it down by category and wallet.
func (s *ReportService) PeriodReport(ctx context.Context, req *connect.Request[yasakuv1.PeriodReportRequest]) (*connect.Response[yasakuv1.PeriodReportResponse], error) {
	sc, nd, err := s.scope.resolve(ctx, req.Msg.GetTarget())
	if err != nil {
		return nil, err
	}
	if nd != nil {
		return nil, needsErr(nd)
	}
	p, nd, err := resolvePeriod(sc.ctx, s.periods, req.Msg.GetPeriod())
	if err != nil {
		return nil, err
	}
	if nd != nil {
		return nil, needsErr(nd)
	}
	sum, err := s.reports.Summary(sc.ctx, p.ID)
	if err != nil {
		return nil, err
	}
	spend, err := s.reports.SpendByCategory(sc.ctx, p.ID)
	if err != nil {
		return nil, err
	}
	income, err := s.reports.IncomeByCategory(sc.ctx, p.ID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&yasakuv1.PeriodReportResponse{
		Period:           toProtoPeriod(p),
		Income:           toMoney(sum.Income),
		Expense:          toMoney(sum.Expense),
		Net:              toMoney(sum.Net),
		TxCount:          int32(sum.TxCount), //nolint:gosec // a row count, far below int32.
		SpendByCategory:  toProtoCategorySlices(spend),
		IncomeByCategory: toProtoCategorySlices(income),
		Wallets:          toProtoWalletLines(sum.Wallets),
	}), nil
}

// CashflowReport returns income, expense and net for the most recent periods, oldest first.
func (s *ReportService) CashflowReport(ctx context.Context, req *connect.Request[yasakuv1.CashflowReportRequest]) (*connect.Response[yasakuv1.CashflowReportResponse], error) {
	sc, nd, err := s.scope.resolve(ctx, req.Msg.GetTarget())
	if err != nil {
		return nil, err
	}
	if nd != nil {
		return nil, needsErr(nd)
	}
	rows, err := s.periods.List(sc.ctx, period.ListOpts{Limit: clampCashflowPeriods(req.Msg.GetPeriods())})
	if err != nil {
		return nil, err
	}
	byID := make(map[uuid.UUID]*period.Period, len(rows))
	ids := make([]uuid.UUID, 0, len(rows))
	for i := len(rows) - 1; i >= 0; i-- {
		byID[rows[i].ID] = rows[i]
		ids = append(ids, rows[i].ID)
	}
	if len(ids) == 0 {
		return connect.NewResponse(&yasakuv1.CashflowReportResponse{}), nil
	}
	points, err := s.reports.Cashflow(sc.ctx, ids)
	if err != nil {
		return nil, err
	}
	out := &yasakuv1.CashflowReportResponse{Points: make([]*yasakuv1.CashflowPoint, 0, len(points))}
	for _, pt := range points {
		out.Points = append(out.Points, &yasakuv1.CashflowPoint{
			Period:  toProtoPeriod(byID[pt.Period.ID]),
			Income:  toMoney(pt.Income),
			Expense: toMoney(pt.Expense),
			Net:     toMoney(pt.Net),
		})
	}
	return connect.NewResponse(out), nil
}

func clampCashflowPeriods(requested int32) int {
	if requested <= 0 {
		return defaultCashflowPeriods
	}
	return min(int(requested), maxCashflowPeriods)
}
