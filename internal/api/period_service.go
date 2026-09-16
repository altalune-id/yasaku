package api

import (
	"context"
	"strings"
	"time"

	"connectrpc.com/connect"

	"altalune.id/yasaku/civil"
	yasakuv1 "altalune.id/yasaku/gen/go/yasaku/v1"
	"altalune.id/yasaku/internal/ledger"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/period"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/report"
	"altalune.id/yasaku/money"
)

// PeriodService implements yasaku.v1.PeriodService.
type PeriodService struct {
	scope   scopeResolver
	periods *period.Service
	reports *report.Service
	ledgers *ledger.Service
	now     func() time.Time
}

// NewPeriodService binds the handler to its collaborators.
func NewPeriodService(orgs *org.Service, projects *project.Service, periods *period.Service, reports *report.Service, ledgers *ledger.Service) *PeriodService {
	return &PeriodService{
		scope:   scopeResolver{orgs: orgs, projects: projects},
		periods: periods,
		reports: reports,
		ledgers: ledgers,
		now:     time.Now,
	}
}

// GetCurrentPeriod returns the open period and its running totals; it never creates one.
func (s *PeriodService) GetCurrentPeriod(ctx context.Context, req *connect.Request[yasakuv1.GetCurrentPeriodRequest]) (*connect.Response[yasakuv1.GetCurrentPeriodResponse], error) {
	sc, nd, err := s.scope.resolve(ctx, req.Msg.GetTarget())
	if err != nil {
		return nil, err
	}
	if nd != nil {
		return nil, needsErr(nd)
	}
	cur, err := s.periods.Current(sc.ctx)
	if err != nil {
		if period.IsNotFoundError(err) {
			return connect.NewResponse(&yasakuv1.GetCurrentPeriodResponse{}), nil
		}
		return nil, err
	}
	sum, err := s.reports.Summary(sc.ctx, cur.ID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&yasakuv1.GetCurrentPeriodResponse{
		Period:  toProtoPeriod(cur),
		Running: summarySnapshot(sum, s.now().UTC()),
	}), nil
}

// ListPeriods returns the project's periods, newest start first.
func (s *PeriodService) ListPeriods(ctx context.Context, req *connect.Request[yasakuv1.ListPeriodsRequest]) (*connect.Response[yasakuv1.ListPeriodsResponse], error) {
	sc, nd, err := s.scope.resolve(ctx, req.Msg.GetTarget())
	if err != nil {
		return nil, err
	}
	if nd != nil {
		return nil, needsErr(nd)
	}
	rows, err := s.periods.List(sc.ctx, period.ListOpts{Limit: int(req.Msg.GetLimit())})
	if err != nil {
		return nil, err
	}
	out := &yasakuv1.ListPeriodsResponse{Periods: make([]*yasakuv1.Period, 0, len(rows))}
	for _, p := range rows {
		out.Periods = append(out.Periods, toProtoPeriod(p))
	}
	return connect.NewResponse(out), nil
}

// PreviewClose computes what closing a period would freeze, persisting nothing.
func (s *PeriodService) PreviewClose(ctx context.Context, req *connect.Request[yasakuv1.PreviewCloseRequest]) (*connect.Response[yasakuv1.PreviewCloseResponse], error) {
	sc, p, end, nd, err := s.target(ctx, req.Msg.GetTarget(), req.Msg.GetPeriod(), req.Msg.GetEndDate())
	if err != nil {
		return nil, err
	}
	if nd != nil {
		return nil, needsErr(nd)
	}
	snap, err := s.periods.PreviewClose(sc.ctx, p.ID, end)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&yasakuv1.PreviewCloseResponse{
		Snapshot: toProtoSnapshot(&snap),
		Period:   toProtoPeriod(p),
	}), nil
}

// ClosePeriod previews the closing snapshot, then on confirm freezes the period.
func (s *PeriodService) ClosePeriod(ctx context.Context, req *connect.Request[yasakuv1.ClosePeriodRequest]) (*connect.Response[yasakuv1.ClosePeriodResponse], error) {
	sc, p, end, nd, err := s.target(ctx, req.Msg.GetTarget(), req.Msg.GetPeriod(), req.Msg.GetEndDate())
	if err != nil {
		return nil, err
	}
	if nd != nil {
		return connect.NewResponse(&yasakuv1.ClosePeriodResponse{Needs: nd}), nil
	}
	if !req.Msg.GetConfirm() {
		snap, pErr := s.periods.PreviewClose(sc.ctx, p.ID, end)
		if pErr != nil {
			return nil, pErr
		}
		return connect.NewResponse(&yasakuv1.ClosePeriodResponse{Preview: toProtoSnapshot(&snap)}), nil
	}
	closed, err := s.periods.Close(sc.ctx, p.ID, end, sc.userID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&yasakuv1.ClosePeriodResponse{Result: toProtoPeriod(closed)}), nil
}

// ReopenPeriod previews the reopened period, then on confirm unlocks it.
func (s *PeriodService) ReopenPeriod(ctx context.Context, req *connect.Request[yasakuv1.ReopenPeriodRequest]) (*connect.Response[yasakuv1.ReopenPeriodResponse], error) {
	sc, nd, err := s.scope.resolve(ctx, req.Msg.GetTarget())
	if err != nil {
		return nil, err
	}
	if nd != nil {
		return connect.NewResponse(&yasakuv1.ReopenPeriodResponse{Needs: nd}), nil
	}
	p, nd, err := s.reopenTarget(sc.ctx, req.Msg.GetPeriod())
	if err != nil {
		return nil, err
	}
	if nd != nil {
		return connect.NewResponse(&yasakuv1.ReopenPeriodResponse{Needs: nd}), nil
	}
	if !req.Msg.GetConfirm() {
		return connect.NewResponse(&yasakuv1.ReopenPeriodResponse{
			Preview: toProtoPeriod(p),
			Warning: "the snapshot stays as it was recorded and goes stale once the period is reopened",
		}), nil
	}
	reopened, err := s.periods.Reopen(sc.ctx, p.ID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&yasakuv1.ReopenPeriodResponse{Result: toProtoPeriod(reopened)}), nil
}

// RenamePeriod replaces the identified period's display name.
func (s *PeriodService) RenamePeriod(ctx context.Context, req *connect.Request[yasakuv1.RenamePeriodRequest]) (*connect.Response[yasakuv1.RenamePeriodResponse], error) {
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
	renamed, err := s.periods.Rename(sc.ctx, p.ID, strings.TrimSpace(req.Msg.GetName()))
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&yasakuv1.RenamePeriodResponse{Period: toProtoPeriod(renamed)}), nil
}

// reopenTarget refuses an empty period rather than resolving it to the current OPEN one, which the
// domain then rejects; the closed periods are offered as candidates instead.
func (s *PeriodService) reopenTarget(ctx context.Context, q string) (*period.Period, *yasakuv1.Needs, error) {
	if strings.TrimSpace(q) == "" {
		rows, err := s.periods.List(ctx, period.ListOpts{})
		if err != nil {
			return nil, nil, err
		}
		ids := make([]string, 0, len(rows))
		for _, p := range rows {
			if p.IsLocked() {
				ids = append(ids, p.ID.String())
			}
		}
		return nil, needs("period", "name the closed period to reopen", ids...), nil
	}
	return resolvePeriod(ctx, s.periods, q)
}

func (s *PeriodService) target(ctx context.Context, t *yasakuv1.Target, periodQ, endRaw string) (scopeResult, *period.Period, civil.Date, *yasakuv1.Needs, error) {
	sc, nd, err := s.scope.resolve(ctx, t)
	if err != nil || nd != nil {
		return scopeResult{}, nil, civil.Date{}, nd, err
	}
	p, nd, err := resolvePeriod(sc.ctx, s.periods, periodQ)
	if err != nil || nd != nil {
		return sc, nil, civil.Date{}, nd, err
	}
	end, err := s.endDate(sc.ctx, endRaw)
	if err != nil {
		return sc, nil, civil.Date{}, nil, err
	}
	return sc, p, end, nil, nil
}

func (s *PeriodService) endDate(ctx context.Context, raw string) (civil.Date, error) {
	if strings.TrimSpace(raw) != "" {
		return parseDate("end_date", raw)
	}
	settings, err := s.ledgers.Get(ctx)
	if err != nil {
		return civil.Date{}, err
	}
	loc, err := settings.Location()
	if err != nil {
		return civil.Date{}, err
	}
	return civil.DateOf(s.now(), loc), nil
}

func summarySnapshot(sum report.PeriodSummary, computedAt time.Time) *yasakuv1.Snapshot {
	out := &yasakuv1.Snapshot{
		Income:     toMoney(sum.Income),
		Expense:    toMoney(sum.Expense),
		Net:        toMoney(sum.Net),
		TxCount:    int32(sum.TxCount), //nolint:gosec // a row count, far below int32.
		ComputedAt: toTimestamp(computedAt),
		Wallets:    make([]*yasakuv1.WalletClosing, 0, len(sum.Wallets)),
	}
	for _, w := range sum.Wallets {
		out.Wallets = append(out.Wallets, &yasakuv1.WalletClosing{
			Wallet:  &yasakuv1.WalletRef{Id: w.WalletID.String(), Name: w.Name},
			Closing: toMoney(money.New(w.Closing.Minor, sum.Currency)),
		})
	}
	return out
}
