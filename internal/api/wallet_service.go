package api

import (
	"context"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	"altalune.id/yasaku/civil"
	yasakuv1 "altalune.id/yasaku/gen/go/yasaku/v1"
	"altalune.id/yasaku/internal/category"
	"altalune.id/yasaku/internal/ledger"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/period"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/report"
	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/internal/wallet"
	"altalune.id/yasaku/money"
)

const recentWalletTransactions = 10

//nolint:gochecknoglobals // a fixed table mirroring the wallet kind CHECK constraint.
var walletKinds = []string{
	string(wallet.KindCash), string(wallet.KindBank), string(wallet.KindEwallet),
	string(wallet.KindSavings), string(wallet.KindInvestment), string(wallet.KindOther),
}

// WalletService implements yasaku.v1.WalletService.
type WalletService struct {
	scope   scopeResolver
	wallets *wallet.Service
	open    *wallet.OpenWorkflow
	txs     *transaction.Service
	cats    *category.Service
	periods *period.Service
	reports *report.Service
	ledgers *ledger.Service
	now     func() time.Time
}

// NewWalletService binds the handler to its collaborators.
func NewWalletService(orgs *org.Service, projects *project.Service, wallets *wallet.Service, open *wallet.OpenWorkflow, txs *transaction.Service, cats *category.Service, periods *period.Service, reports *report.Service, ledgers *ledger.Service) *WalletService {
	return &WalletService{
		scope:   scopeResolver{orgs: orgs, projects: projects},
		wallets: wallets,
		open:    open,
		txs:     txs,
		cats:    cats,
		periods: periods,
		reports: reports,
		ledgers: ledgers,
		now:     time.Now,
	}
}

// ListWallets returns the project's wallets with their derived balances.
func (s *WalletService) ListWallets(ctx context.Context, req *connect.Request[yasakuv1.ListWalletsRequest]) (*connect.Response[yasakuv1.ListWalletsResponse], error) {
	sc, nd, err := s.scope.resolve(ctx, req.Msg.GetTarget())
	if err != nil {
		return nil, err
	}
	if nd != nil {
		return nil, needsErr(nd)
	}
	rows, err := s.wallets.List(sc.ctx, wallet.ListOpts{IncludeArchived: req.Msg.GetIncludeArchived()})
	if err != nil {
		return nil, err
	}
	balances, err := s.txs.Balances(sc.ctx)
	if err != nil {
		return nil, err
	}
	out := &yasakuv1.ListWalletsResponse{Wallets: make([]*yasakuv1.Wallet, 0, len(rows))}
	for _, w := range rows {
		out.Wallets = append(out.Wallets, toProtoWallet(w, balanceOf(balances, w)))
	}
	return connect.NewResponse(out), nil
}

// GetWallet returns one wallet with its most recent transactions.
func (s *WalletService) GetWallet(ctx context.Context, req *connect.Request[yasakuv1.GetWalletRequest]) (*connect.Response[yasakuv1.GetWalletResponse], error) {
	sc, w, nd, err := s.lookup(ctx, req.Msg.GetTarget(), req.Msg.GetWallet())
	if err != nil {
		return nil, err
	}
	if nd != nil {
		return nil, needsErr(nd)
	}
	balance, err := s.txs.Balance(sc.ctx, w.ID)
	if err != nil {
		return nil, err
	}
	rows, _, err := s.txs.List(sc.ctx, transaction.ListOpts{WalletID: &w.ID, Limit: recentWalletTransactions})
	if err != nil {
		return nil, err
	}
	loc, err := s.location(sc.ctx)
	if err != nil {
		return nil, err
	}
	refs := newRefCache(s.wallets, s.cats, s.periods, loc)
	return connect.NewResponse(&yasakuv1.GetWalletResponse{
		Wallet: toProtoWallet(w, &balance),
		Recent: refs.txs(sc.ctx, rows),
	}), nil
}

// CreateWallet previews the resolved wallet, then on confirm opens it with its opening balance.
func (s *WalletService) CreateWallet(ctx context.Context, req *connect.Request[yasakuv1.CreateWalletRequest]) (*connect.Response[yasakuv1.CreateWalletResponse], error) {
	sc, nd, err := s.scope.resolve(ctx, req.Msg.GetTarget())
	if err != nil {
		return nil, err
	}
	if nd != nil {
		return connect.NewResponse(&yasakuv1.CreateWalletResponse{Needs: nd}), nil
	}
	name := strings.TrimSpace(req.Msg.GetName())
	if name == "" {
		return connect.NewResponse(&yasakuv1.CreateWalletResponse{Needs: needs("name", "name the wallet")}), nil
	}
	kind, ok := parseWalletKind(req.Msg.GetKind())
	if !ok {
		return connect.NewResponse(&yasakuv1.CreateWalletResponse{
			Needs: needs("kind", "kind must be one of the supported wallet kinds", walletKinds...),
		}), nil
	}
	currency, err := s.currency(sc.ctx, req.Msg.GetCurrency())
	if err != nil {
		return nil, err
	}
	exclude := kind.DefaultExcludeFromTotal()
	if v := req.Msg.ExcludeFromTotal; v != nil {
		exclude = *v
	}
	params := wallet.Params{
		Name:             name,
		Kind:             kind,
		Provider:         strings.TrimSpace(req.Msg.GetProvider()),
		Currency:         currency,
		ExcludeFromTotal: exclude,
	}

	var opening *money.Amount
	if req.Msg.GetOpeningBalance() != nil {
		amount, pErr := parseMoney(req.Msg.GetOpeningBalance(), currency)
		if pErr != nil {
			return nil, pErr
		}
		opening = &amount
	}

	if !req.Msg.GetConfirm() {
		balance := money.Zero(currency)
		if opening != nil {
			balance = *opening
		}
		return connect.NewResponse(&yasakuv1.CreateWalletResponse{
			Preview: &yasakuv1.Wallet{
				Name:             params.Name,
				Kind:             string(params.Kind),
				Provider:         params.Provider,
				Currency:         string(params.Currency),
				ExcludeFromTotal: params.ExcludeFromTotal,
				Balance:          toMoney(balance),
			},
		}), nil
	}

	var created *wallet.Wallet
	if opening != nil {
		created, err = s.open.Run(sc.ctx, params, opening, s.now())
	} else {
		created, err = s.wallets.Create(sc.ctx, params)
	}
	if err != nil {
		return nil, err
	}
	balance, err := s.txs.Balance(sc.ctx, created.ID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&yasakuv1.CreateWalletResponse{Result: toProtoWallet(created, &balance)}), nil
}

// UpdateWallet previews the stored wallet with the requested changes applied, then on confirm saves them.
func (s *WalletService) UpdateWallet(ctx context.Context, req *connect.Request[yasakuv1.UpdateWalletRequest]) (*connect.Response[yasakuv1.UpdateWalletResponse], error) {
	sc, w, nd, err := s.lookup(ctx, req.Msg.GetTarget(), req.Msg.GetWallet())
	if err != nil {
		return nil, err
	}
	if nd != nil {
		return connect.NewResponse(&yasakuv1.UpdateWalletResponse{Needs: nd}), nil
	}
	kind := w.Kind
	if v := req.Msg.Kind; v != nil {
		parsed, ok := parseWalletKind(*v)
		if !ok {
			return connect.NewResponse(&yasakuv1.UpdateWalletResponse{
				Needs: needs("kind", "kind must be one of the supported wallet kinds", walletKinds...),
			}), nil
		}
		kind = parsed
	}
	provider := w.Provider
	if v := req.Msg.Provider; v != nil {
		provider = strings.TrimSpace(*v)
	}
	exclude := w.ExcludeFromTotal
	if v := req.Msg.ExcludeFromTotal; v != nil {
		exclude = *v
	}
	name := w.Name
	if v := req.Msg.Name; v != nil {
		name = strings.TrimSpace(*v)
	}
	// SECURITY: Update and Rename are two independent writes with no unit of work between them, so a
	// rename the aggregate rejects is caught here, and Rename runs first so one the store rejects as
	// taken fails before the other three fields are written.
	if err := s.checkRename(w, name); err != nil {
		return nil, err
	}
	balance, err := s.txs.Balance(sc.ctx, w.ID)
	if err != nil {
		return nil, err
	}

	if !req.Msg.GetConfirm() {
		preview := toProtoWallet(w, &balance)
		preview.Name, preview.Kind, preview.Provider, preview.ExcludeFromTotal = name, string(kind), provider, exclude
		return connect.NewResponse(&yasakuv1.UpdateWalletResponse{Preview: preview}), nil
	}

	if name != w.Name {
		if _, rErr := s.wallets.Rename(sc.ctx, w.ID, name); rErr != nil {
			return nil, rErr
		}
	}
	updated, err := s.wallets.Update(sc.ctx, w.ID, kind, provider, exclude)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&yasakuv1.UpdateWalletResponse{Result: toProtoWallet(updated, &balance)}), nil
}

// ArchiveWallet previews the stored wallet, then on confirm archives it.
func (s *WalletService) ArchiveWallet(ctx context.Context, req *connect.Request[yasakuv1.ArchiveWalletRequest]) (*connect.Response[yasakuv1.ArchiveWalletResponse], error) {
	sc, w, nd, err := s.lookup(ctx, req.Msg.GetTarget(), req.Msg.GetWallet())
	if err != nil {
		return nil, err
	}
	if nd != nil {
		return connect.NewResponse(&yasakuv1.ArchiveWalletResponse{Needs: nd}), nil
	}
	balance, err := s.txs.Balance(sc.ctx, w.ID)
	if err != nil {
		return nil, err
	}
	if !req.Msg.GetConfirm() {
		return connect.NewResponse(&yasakuv1.ArchiveWalletResponse{Preview: toProtoWallet(w, &balance)}), nil
	}
	archived, err := s.wallets.Archive(sc.ctx, w.ID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&yasakuv1.ArchiveWalletResponse{Result: toProtoWallet(archived, &balance)}), nil
}

// UnarchiveWallet restores an archived wallet.
func (s *WalletService) UnarchiveWallet(ctx context.Context, req *connect.Request[yasakuv1.UnarchiveWalletRequest]) (*connect.Response[yasakuv1.UnarchiveWalletResponse], error) {
	sc, w, nd, err := s.lookupArchived(ctx, req.Msg.GetTarget(), req.Msg.GetWallet())
	if err != nil {
		return nil, err
	}
	if nd != nil {
		return nil, needsErr(nd)
	}
	restored, err := s.wallets.Unarchive(sc.ctx, w.ID)
	if err != nil {
		return nil, err
	}
	balance, err := s.txs.Balance(sc.ctx, w.ID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&yasakuv1.UnarchiveWalletResponse{Wallet: toProtoWallet(restored, &balance)}), nil
}

// DeleteWallet removes a wallet no transaction references.
func (s *WalletService) DeleteWallet(ctx context.Context, req *connect.Request[yasakuv1.DeleteWalletRequest]) (*connect.Response[yasakuv1.DeleteWalletResponse], error) {
	sc, w, nd, err := s.lookup(ctx, req.Msg.GetTarget(), req.Msg.GetWallet())
	if err != nil {
		return nil, err
	}
	if nd != nil {
		return nil, needsErr(nd)
	}
	if err := s.wallets.Delete(sc.ctx, w.ID); err != nil {
		return nil, err
	}
	return connect.NewResponse(&yasakuv1.DeleteWalletResponse{}), nil
}

// AdjustBalance previews the adjustment that would reconcile the wallet, then on confirm writes it.
func (s *WalletService) AdjustBalance(ctx context.Context, req *connect.Request[yasakuv1.AdjustBalanceRequest]) (*connect.Response[yasakuv1.AdjustBalanceResponse], error) {
	sc, w, nd, err := s.lookup(ctx, req.Msg.GetTarget(), req.Msg.GetWallet())
	if err != nil {
		return nil, err
	}
	if nd != nil {
		return connect.NewResponse(&yasakuv1.AdjustBalanceResponse{Needs: nd}), nil
	}
	target, err := parseMoney(req.Msg.GetTargetBalance(), w.Currency)
	if err != nil {
		return nil, err
	}
	loc, err := s.location(sc.ctx)
	if err != nil {
		return nil, err
	}
	at, err := parseWhen("date", req.Msg.GetDate(), loc, s.now())
	if err != nil {
		return nil, err
	}

	if !req.Msg.GetConfirm() {
		current, bErr := s.txs.Balance(sc.ctx, w.ID)
		if bErr != nil {
			return nil, bErr
		}
		delta := target.Minor - current.Minor
		if delta == 0 {
			return connect.NewResponse(&yasakuv1.AdjustBalanceResponse{
				Warning: "the wallet already holds that balance; nothing would be written",
			}), nil
		}
		kind := transaction.KindAdjustmentIn
		if delta < 0 {
			kind, delta = transaction.KindAdjustmentOut, -delta
		}
		return connect.NewResponse(&yasakuv1.AdjustBalanceResponse{
			Preview: &yasakuv1.Transaction{
				Kind:       string(kind),
				Wallet:     toWalletRef(w),
				Amount:     toMoney(money.New(delta, w.Currency)),
				OccurredAt: toTimestamp(at),
				Date:       civil.DateOf(at, loc).String(),
			},
			Warning: "the balance is read again under a lock at confirm time, so a write landing in between changes the adjustment",
		}), nil
	}

	t, err := s.txs.Adjust(sc.ctx, w.ID, target, at, sc.userID)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return connect.NewResponse(&yasakuv1.AdjustBalanceResponse{
			Warning: "the wallet already held that balance; nothing was written",
		}), nil
	}
	refs := newRefCache(s.wallets, s.cats, s.periods, loc)
	return connect.NewResponse(&yasakuv1.AdjustBalanceResponse{Result: refs.tx(sc.ctx, t)}), nil
}

// WalletTotals reports the spendable and overall totals alongside the current period's flows.
func (s *WalletService) WalletTotals(ctx context.Context, req *connect.Request[yasakuv1.WalletTotalsRequest]) (*connect.Response[yasakuv1.WalletTotalsResponse], error) {
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
			return s.totalsWithoutPeriod(sc.ctx)
		}
		return nil, err
	}
	sum, err := s.reports.Summary(sc.ctx, cur.ID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&yasakuv1.WalletTotalsResponse{
		SpendableTotal: toMoney(sum.SpendableTotal),
		Total:          toMoney(sum.Total),
		Income:         toMoney(sum.Income),
		Expense:        toMoney(sum.Expense),
		Net:            toMoney(sum.Net),
		Period:         toProtoPeriod(cur),
		Wallets:        toProtoWalletLines(sum.Wallets),
	}), nil
}

// totalsWithoutPeriod answers a project that has never recorded a transaction, so has no period.
func (s *WalletService) totalsWithoutPeriod(ctx context.Context) (*connect.Response[yasakuv1.WalletTotalsResponse], error) {
	lines, err := s.reports.WalletBalances(ctx)
	if err != nil {
		return nil, err
	}
	settings, err := s.ledgers.Get(ctx)
	if err != nil {
		return nil, err
	}
	zero := toMoney(money.Zero(settings.Currency))
	spendable, total := money.Zero(settings.Currency), money.Zero(settings.Currency)
	for _, l := range lines {
		if l.Closing.Currency != settings.Currency {
			continue
		}
		total = total.Add(l.Closing)
		if !l.ExcludeFromTotal {
			spendable = spendable.Add(l.Closing)
		}
	}
	return connect.NewResponse(&yasakuv1.WalletTotalsResponse{
		SpendableTotal: toMoney(spendable),
		Total:          toMoney(total),
		Income:         zero,
		Expense:        zero,
		Net:            zero,
		Wallets:        toProtoWalletLines(lines),
	}), nil
}

func (s *WalletService) lookup(ctx context.Context, t *yasakuv1.Target, q string) (scopeResult, *wallet.Wallet, *yasakuv1.Needs, error) {
	sc, nd, err := s.scope.resolve(ctx, t)
	if err != nil || nd != nil {
		return scopeResult{}, nil, nd, err
	}
	w, nd, err := resolveWallet(sc.ctx, s.wallets, "wallet", q)
	return sc, w, nd, err
}

func (s *WalletService) lookupArchived(ctx context.Context, t *yasakuv1.Target, q string) (scopeResult, *wallet.Wallet, *yasakuv1.Needs, error) {
	sc, nd, err := s.scope.resolve(ctx, t)
	if err != nil || nd != nil {
		return scopeResult{}, nil, nd, err
	}
	w, nd, err := resolveArchivedWallet(sc.ctx, s.wallets, "wallet", q)
	return sc, w, nd, err
}

// checkRename refuses every rename the two-step update would otherwise half-commit: an archived
// wallet, a name the aggregate rejects, and a name another active wallet already holds.
func (s *WalletService) checkRename(w *wallet.Wallet, name string) error {
	if name == w.Name {
		return nil
	}
	if w.IsArchived() {
		return &wallet.ArchivedError{ID: w.ID.String()}
	}
	probe := *w
	return probe.Rename(name)
}

func (s *WalletService) currency(ctx context.Context, raw string) (money.Currency, error) {
	if code := strings.TrimSpace(raw); code != "" {
		cur, err := money.ParseCurrency(code)
		if err != nil {
			return "", invalidArgCause("currency", "unknown currency "+code, err)
		}
		return cur, nil
	}
	settings, err := s.ledgers.Get(ctx)
	if err != nil {
		return "", err
	}
	return settings.Currency, nil
}

func (s *WalletService) location(ctx context.Context) (*time.Location, error) {
	settings, err := s.ledgers.Get(ctx)
	if err != nil {
		return nil, err
	}
	return settings.Location()
}

func parseWalletKind(raw string) (wallet.Kind, bool) {
	kind, err := wallet.ParseKind(strings.TrimSpace(raw))
	return kind, err == nil
}

func balanceOf(balances map[uuid.UUID]money.Amount, w *wallet.Wallet) *money.Amount {
	if b, ok := balances[w.ID]; ok {
		return &b
	}
	zero := money.Zero(w.Currency)
	return &zero
}
