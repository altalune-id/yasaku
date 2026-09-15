package boot

import (
	"context"
	"time"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/category"
	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/period"
	pdb "altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/report"
	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/internal/wallet"
)

type snapshotterAdapter struct {
	reports *report.Service
	now     func() time.Time
}

var _ period.Snapshotter = snapshotterAdapter{}

// Snapshot stamps ComputedAt here because period.Close persists the document verbatim into both the period row and its immutable closing record.
func (a snapshotterAdapter) Snapshot(ctx context.Context, orgID, projectID, periodID uuid.UUID) (period.Snapshot, error) {
	sum, err := a.reports.SnapshotFor(ctx, orgID, projectID, periodID)
	if err != nil {
		return period.Snapshot{}, err
	}
	now := a.now
	if now == nil {
		now = time.Now
	}
	snap := period.Snapshot{
		Currency:   sum.Currency,
		Income:     sum.Income.Minor,
		Expense:    sum.Expense.Minor,
		Net:        sum.Net.Minor,
		TxCount:    sum.TxCount,
		ComputedAt: now().UTC(),
	}
	if len(sum.Wallets) > 0 {
		snap.Wallets = make([]period.WalletClosing, 0, len(sum.Wallets))
		for _, w := range sum.Wallets {
			snap.Wallets = append(snap.Wallets, period.WalletClosing{
				WalletID: w.WalletID,
				Name:     w.Name,
				Closing:  w.Closing.Minor,
			})
		}
	}
	return snap, nil
}

type periodResolverAdapter struct{ periods *period.Service }

var _ transaction.PeriodResolver = periodResolverAdapter{}

// Containing reports the period covering at; a project with no such period yields ok == false, not an error.
func (a periodResolverAdapter) Containing(ctx context.Context, _, _ uuid.UUID, at time.Time) (transaction.PeriodInfo, bool, error) {
	p, err := a.periods.Containing(ctx, at)
	if err != nil {
		if period.IsNotFoundError(err) {
			return transaction.PeriodInfo{}, false, nil
		}
		return transaction.PeriodInfo{}, false, err
	}
	info, err := a.info(ctx, p)
	if err != nil {
		return transaction.PeriodInfo{}, false, err
	}
	return info, true, nil
}

func (a periodResolverAdapter) ByID(ctx context.Context, _, _, id uuid.UUID) (transaction.PeriodInfo, error) {
	p, err := a.periods.ByID(ctx, id)
	if err != nil {
		return transaction.PeriodInfo{}, err
	}
	return a.info(ctx, p)
}

func (a periodResolverAdapter) info(ctx context.Context, p *period.Period) (transaction.PeriodInfo, error) {
	prev, next, err := a.periods.Neighbors(ctx, p.ID)
	if err != nil {
		return transaction.PeriodInfo{}, err
	}
	info := transaction.PeriodInfo{ID: p.ID, Locked: p.IsLocked()}
	if prev != nil {
		id := prev.ID
		info.PrevID = &id
	}
	if next != nil {
		id := next.ID
		info.NextID = &id
	}
	return info, nil
}

type namerAdapter struct{}

var _ category.Namer = namerAdapter{}

// DefaultName resolves a seeded category's message id; with no translator on ctx it reports nothing, so category falls back to its own title-cased key rather than persisting the raw message id.
func (namerAdapter) DefaultName(ctx context.Context, key string) string {
	t := i18n.TranslatorFrom(ctx)
	if t == nil {
		return ""
	}
	return t.T(key)
}

// walletReaderFor resolves a wallet through the wallet service on the caller's already-scoped context.
// SECURITY: the adapter never fabricates tenant scope; it asserts the caller's scope matches the row and reports a mismatch as absent.
func walletReaderFor(wallets *wallet.Service) transaction.WalletReaderFunc {
	return func(ctx context.Context, orgID, projectID, id uuid.UUID) (transaction.WalletInfo, error) {
		w, err := wallets.ByID(ctx, id)
		if err != nil {
			return transaction.WalletInfo{}, err
		}
		if w.OrgID != orgID || w.ProjectID != projectID {
			return transaction.WalletInfo{}, &wallet.NotFoundError{ID: id.String()}
		}
		return transaction.WalletInfo{ID: w.ID, Currency: w.Currency, Archived: w.IsArchived()}, nil
	}
}

// categoryReaderFor resolves a category through the category service on the caller's already-scoped context.
// SECURITY: the adapter never fabricates tenant scope; it asserts the caller's scope matches the row and reports a mismatch as absent.
func categoryReaderFor(categories *category.Service) transaction.CategoryReaderFunc {
	return func(ctx context.Context, orgID, projectID, id uuid.UUID) (transaction.CategoryInfo, error) {
		c, err := categories.ByID(ctx, id)
		if err != nil {
			return transaction.CategoryInfo{}, err
		}
		if c.OrgID != orgID || c.ProjectID != projectID {
			return transaction.CategoryInfo{}, &category.NotFoundError{ID: id.String()}
		}
		return transaction.CategoryInfo{ID: c.ID, Kind: string(c.Kind), Archived: c.IsArchived()}, nil
	}
}

// unitOfWork builds the real transaction boundary the wallet, period and transaction modules share.
func unitOfWork(cfg pdb.DBConfig, pool pdb.Pool, pc *tenant.PgConn) func(context.Context, func(context.Context) error) error {
	if cfg.Driver == pdb.DriverPostgres {
		return func(ctx context.Context, fn func(context.Context) error) error {
			tc, err := tenant.From(ctx)
			if err != nil {
				return err
			}
			return tenant.RunInTx(ctx, pc, tc, fn)
		}
	}
	return func(ctx context.Context, fn func(context.Context) error) error {
		return pdb.RunInTx(ctx, pool, fn)
	}
}
