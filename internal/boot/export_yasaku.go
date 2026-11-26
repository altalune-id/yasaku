package boot

import (
	"context"

	"altalune.id/yasaku/internal/category"
	"altalune.id/yasaku/internal/period"
	"altalune.id/yasaku/internal/report"
	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/internal/wallet"
)

type exportSource struct {
	wallets *wallet.Service
	cats    *category.Service
	txs     *transaction.Service
	periods *period.Service
}

func (s exportSource) Wallets(ctx context.Context) ([]report.ExportWallet, error) {
	ws, err := s.wallets.List(ctx, wallet.ListOpts{IncludeArchived: true})
	if err != nil {
		return nil, err
	}
	out := make([]report.ExportWallet, 0, len(ws))
	for _, w := range ws {
		out = append(out, report.ExportWallet{ID: w.ID, Name: w.Name, Kind: string(w.Kind), Provider: w.Provider, Currency: w.Currency,
			ExcludeFromTotal: w.ExcludeFromTotal, CreatedAt: w.CreatedAt, ArchivedAt: w.ArchivedAt})
	}
	return out, nil
}

func (s exportSource) Categories(ctx context.Context) ([]report.ExportCategory, error) {
	cs, err := s.cats.List(ctx, category.ListOpts{IncludeArchived: true})
	if err != nil {
		return nil, err
	}
	out := make([]report.ExportCategory, 0, len(cs))
	for _, c := range cs {
		out = append(out, report.ExportCategory{ID: c.ID, Name: c.Name, Kind: string(c.Kind), CreatedAt: c.CreatedAt, ArchivedAt: c.ArchivedAt})
	}
	return out, nil
}

func (s exportSource) Transactions(ctx context.Context) ([]report.ExportTransaction, error) {
	var out []report.ExportTransaction
	opts := transaction.ListOpts{Limit: transaction.MaxListLimit}
	for {
		page, next, err := s.txs.List(ctx, opts)
		if err != nil {
			return nil, err
		}
		for _, t := range page {
			out = append(out, report.ExportTransaction{ID: t.ID, OccurredAt: t.OccurredAt, Kind: string(t.Kind), Amount: t.Amount,
				WalletID: t.WalletID, ToWalletID: t.ToWalletID, CategoryID: t.CategoryID, PeriodID: t.PeriodID, Note: t.Note})
		}
		if next == nil {
			return out, nil
		}
		opts.After = next
	}
}

func (s exportSource) Periods(ctx context.Context) ([]report.ExportPeriod, error) {
	ps, err := s.periods.List(ctx, period.ListOpts{})
	if err != nil {
		return nil, err
	}
	out := make([]report.ExportPeriod, 0, len(ps))
	for _, p := range ps {
		out = append(out, report.ExportPeriod{ID: p.ID, Name: p.Name, Start: p.StartDate, End: p.EndDate, ClosedAt: p.ClosedAt})
	}
	return out, nil
}
