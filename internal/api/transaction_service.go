package api

import (
	"context"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	yasakuv1 "altalune.id/yasaku/gen/go/yasaku/v1"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/category"
	"altalune.id/yasaku/internal/ledger"
	"altalune.id/yasaku/internal/org"
	"altalune.id/yasaku/internal/period"
	"altalune.id/yasaku/internal/project"
	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/internal/wallet"
)

// TransactionService implements yasaku.v1.TransactionService.
type TransactionService struct {
	scope   scopeResolver
	txs     *transaction.Service
	wallets *wallet.Service
	cats    *category.Service
	periods *period.Service
	ledgers *ledger.Service
	now     func() time.Time
}

// NewTransactionService binds the handler to its collaborators.
func NewTransactionService(orgs *org.Service, projects *project.Service, txs *transaction.Service, wallets *wallet.Service, cats *category.Service, periods *period.Service, ledgers *ledger.Service) *TransactionService {
	return &TransactionService{
		scope:   scopeResolver{orgs: orgs, projects: projects},
		txs:     txs,
		wallets: wallets,
		cats:    cats,
		periods: periods,
		ledgers: ledgers,
		now:     time.Now,
	}
}

type recordSpec struct {
	kind       transaction.Kind
	wallet     string
	toWallet   string
	amount     *yasakuv1.Money
	categoryQ  string
	note       string
	occurredAt string
	periodQ    string
}

type recordPlan struct {
	input   transaction.RecordInput
	preview *yasakuv1.Transaction
	warning string
}

// RecordExpense previews the resolved expense, then on confirm saves it.
func (s *TransactionService) RecordExpense(ctx context.Context, req *connect.Request[yasakuv1.RecordExpenseRequest]) (*connect.Response[yasakuv1.RecordExpenseResponse], error) {
	sc, plan, nd, err := s.plan(ctx, req.Msg.GetTarget(), recordSpec{
		kind:       transaction.KindExpense,
		wallet:     req.Msg.GetWallet(),
		amount:     req.Msg.GetAmount(),
		categoryQ:  req.Msg.GetCategory(),
		note:       req.Msg.GetNote(),
		occurredAt: req.Msg.GetOccurredAt(),
		periodQ:    req.Msg.GetPeriod(),
	})
	if err != nil {
		return nil, err
	}
	if nd != nil {
		return connect.NewResponse(&yasakuv1.RecordExpenseResponse{Needs: nd}), nil
	}
	if !req.Msg.GetConfirm() {
		return connect.NewResponse(&yasakuv1.RecordExpenseResponse{Preview: plan.preview, Warning: plan.warning}), nil
	}
	t, err := s.commit(sc, plan)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&yasakuv1.RecordExpenseResponse{Result: t}), nil
}

// RecordIncome previews the resolved income, then on confirm saves it.
func (s *TransactionService) RecordIncome(ctx context.Context, req *connect.Request[yasakuv1.RecordIncomeRequest]) (*connect.Response[yasakuv1.RecordIncomeResponse], error) {
	sc, plan, nd, err := s.plan(ctx, req.Msg.GetTarget(), recordSpec{
		kind:       transaction.KindIncome,
		wallet:     req.Msg.GetWallet(),
		amount:     req.Msg.GetAmount(),
		categoryQ:  req.Msg.GetCategory(),
		note:       req.Msg.GetNote(),
		occurredAt: req.Msg.GetOccurredAt(),
		periodQ:    req.Msg.GetPeriod(),
	})
	if err != nil {
		return nil, err
	}
	if nd != nil {
		return connect.NewResponse(&yasakuv1.RecordIncomeResponse{Needs: nd}), nil
	}
	if !req.Msg.GetConfirm() {
		return connect.NewResponse(&yasakuv1.RecordIncomeResponse{Preview: plan.preview, Warning: plan.warning}), nil
	}
	t, err := s.commit(sc, plan)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&yasakuv1.RecordIncomeResponse{Result: t}), nil
}

// RecordTransfer previews the resolved transfer, then on confirm saves it.
func (s *TransactionService) RecordTransfer(ctx context.Context, req *connect.Request[yasakuv1.RecordTransferRequest]) (*connect.Response[yasakuv1.RecordTransferResponse], error) {
	sc, plan, nd, err := s.plan(ctx, req.Msg.GetTarget(), recordSpec{
		kind:       transaction.KindTransfer,
		wallet:     req.Msg.GetFromWallet(),
		toWallet:   req.Msg.GetToWallet(),
		amount:     req.Msg.GetAmount(),
		note:       req.Msg.GetNote(),
		occurredAt: req.Msg.GetOccurredAt(),
		periodQ:    req.Msg.GetPeriod(),
	})
	if err != nil {
		return nil, err
	}
	if nd != nil {
		return connect.NewResponse(&yasakuv1.RecordTransferResponse{Needs: nd}), nil
	}
	if !req.Msg.GetConfirm() {
		return connect.NewResponse(&yasakuv1.RecordTransferResponse{Preview: plan.preview, Warning: plan.warning}), nil
	}
	t, err := s.commit(sc, plan)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&yasakuv1.RecordTransferResponse{Result: t}), nil
}

// RecordBatch previews every item, then on confirm records each one independently.
func (s *TransactionService) RecordBatch(ctx context.Context, req *connect.Request[yasakuv1.RecordBatchRequest]) (*connect.Response[yasakuv1.RecordBatchResponse], error) {
	sc, nd, err := s.scope.resolve(ctx, req.Msg.GetTarget())
	if err != nil {
		return nil, err
	}
	if nd != nil {
		return connect.NewResponse(&yasakuv1.RecordBatchResponse{Needs: nd}), nil
	}
	items := req.Msg.GetItems()
	if len(items) == 0 {
		return connect.NewResponse(&yasakuv1.RecordBatchResponse{Needs: needs("items", "the batch is empty")}), nil
	}

	outcomes := make([]*yasakuv1.BatchOutcome, 0, len(items))
	plans := make([]*recordPlan, len(items))
	for i, item := range items {
		kind, kErr := batchKind(item.GetKind())
		if kErr != nil {
			outcomes = append(outcomes, failedOutcome(i, kErr))
			continue
		}
		plan, iNd, pErr := s.planIn(sc, recordSpec{
			kind:       kind,
			wallet:     item.GetWallet(),
			toWallet:   item.GetToWallet(),
			amount:     item.GetAmount(),
			categoryQ:  item.GetCategory(),
			note:       item.GetNote(),
			occurredAt: item.GetOccurredAt(),
			periodQ:    item.GetPeriod(),
		})
		switch {
		case pErr != nil:
			outcomes = append(outcomes, failedOutcome(i, pErr))
		case iNd != nil:
			outcomes = append(outcomes, needsOutcome(i, iNd))
		default:
			plans[i] = plan
			outcomes = append(outcomes, &yasakuv1.BatchOutcome{
				Index:       int32(i), //nolint:gosec // bounded by len(items).
				Transaction: plan.preview,
			})
		}
	}

	if !req.Msg.GetConfirm() {
		return connect.NewResponse(&yasakuv1.RecordBatchResponse{
			Preview: outcomes,
			Warning: "each item is recorded on its own; a refused item does not undo the rest",
		}), nil
	}

	inputs := make([]transaction.RecordInput, 0, len(plans))
	indexOf := make([]int, 0, len(plans))
	for i, plan := range plans {
		if plan == nil {
			continue
		}
		inputs = append(inputs, plan.input)
		indexOf = append(indexOf, i)
	}
	results := s.txs.RecordBatch(sc.ctx, inputs)
	refs := newRefCache(s.wallets, s.cats, s.periods)
	for _, r := range results {
		i := indexOf[r.Index]
		switch {
		case r.Err != nil:
			outcomes[i] = failedOutcome(i, r.Err)
		default:
			outcomes[i] = &yasakuv1.BatchOutcome{
				Index:       int32(i), //nolint:gosec // bounded by len(items).
				Transaction: refs.tx(sc.ctx, r.Transaction),
			}
		}
	}
	return connect.NewResponse(&yasakuv1.RecordBatchResponse{Results: outcomes}), nil
}

// ReviseTransaction previews the stored transaction with the requested changes, then on confirm saves them.
func (s *TransactionService) ReviseTransaction(ctx context.Context, req *connect.Request[yasakuv1.ReviseTransactionRequest]) (*connect.Response[yasakuv1.ReviseTransactionResponse], error) {
	sc, nd, err := s.scope.resolve(ctx, req.Msg.GetTarget())
	if err != nil {
		return nil, err
	}
	if nd != nil {
		return connect.NewResponse(&yasakuv1.ReviseTransactionResponse{Needs: nd}), nil
	}
	id, err := uuid.Parse(strings.TrimSpace(req.Msg.GetId()))
	if err != nil {
		return nil, invalidArgCause("id", "id must be a transaction id", err)
	}
	stored, err := s.txs.ByID(sc.ctx, id)
	if err != nil {
		return nil, err
	}
	loc, err := s.location(sc.ctx)
	if err != nil {
		return nil, err
	}

	patch := transaction.RevisePatch{}
	preview := newRefCache(s.wallets, s.cats, s.periods).tx(sc.ctx, stored)

	if v := req.Msg.Wallet; v != nil {
		w, wNd, wErr := resolveWallet(sc.ctx, s.wallets, "wallet", *v)
		if wErr != nil || wNd != nil {
			return respondReviseNeeds(wNd, wErr)
		}
		patch.WalletID = &w.ID
		preview.Wallet = toWalletRef(w)
	}
	if v := req.Msg.ToWallet; v != nil {
		if strings.TrimSpace(*v) == "" {
			var none *uuid.UUID
			patch.ToWalletID = &none
			preview.ToWallet = nil
		} else {
			w, wNd, wErr := resolveWallet(sc.ctx, s.wallets, "to_wallet", *v)
			if wErr != nil || wNd != nil {
				return respondReviseNeeds(wNd, wErr)
			}
			id := w.ID
			ptr := &id
			patch.ToWalletID = &ptr
			preview.ToWallet = toWalletRef(w)
		}
	}
	if m := req.Msg.GetAmount(); m != nil {
		amount, aErr := parseMoney(m, stored.Amount.Currency)
		if aErr != nil {
			return nil, aErr
		}
		patch.Amount = &amount
		preview.Amount = toMoney(amount)
	}
	if v := req.Msg.Category; v != nil {
		if strings.TrimSpace(*v) == "" {
			var none *uuid.UUID
			patch.CategoryID = &none
			preview.Category = nil
		} else {
			kind, kErr := categoryKindFor(stored.Kind)
			if kErr != nil {
				return nil, kErr
			}
			c, cNd, cErr := resolveCategory(sc.ctx, s.cats, kind, "category", *v)
			if cErr != nil || cNd != nil {
				return respondReviseNeeds(cNd, cErr)
			}
			id := c.ID
			ptr := &id
			patch.CategoryID = &ptr
			preview.Category = toProtoCategory(c)
		}
	}
	if v := req.Msg.Note; v != nil {
		note := strings.TrimSpace(*v)
		patch.Note = &note
		preview.Note = note
	}
	if v := req.Msg.OccurredAt; v != nil {
		at, tErr := parseWhen("occurred_at", *v, loc, s.now())
		if tErr != nil {
			return nil, tErr
		}
		patch.OccurredAt = &at
		preview.OccurredAt = toTimestamp(at)
	}
	if v := req.Msg.Period; v != nil {
		if strings.TrimSpace(*v) == "" {
			var none *uuid.UUID
			patch.PeriodID = &none
			preview.PeriodId, preview.PeriodName = "", ""
		} else {
			p, pNd, pErr := resolvePeriod(sc.ctx, s.periods, *v)
			if pErr != nil || pNd != nil {
				return respondReviseNeeds(pNd, pErr)
			}
			id := p.ID
			ptr := &id
			patch.PeriodID = &ptr
			preview.PeriodId, preview.PeriodName = p.ID.String(), p.Name
		}
	}

	if !req.Msg.GetConfirm() {
		return connect.NewResponse(&yasakuv1.ReviseTransactionResponse{Preview: preview}), nil
	}
	revised, err := s.txs.Revise(sc.ctx, id, patch)
	if err != nil {
		return nil, err
	}
	refs := newRefCache(s.wallets, s.cats, s.periods)
	return connect.NewResponse(&yasakuv1.ReviseTransactionResponse{Result: refs.tx(sc.ctx, revised)}), nil
}

// DeleteTransaction previews the stored transaction, then on confirm removes it.
func (s *TransactionService) DeleteTransaction(ctx context.Context, req *connect.Request[yasakuv1.DeleteTransactionRequest]) (*connect.Response[yasakuv1.DeleteTransactionResponse], error) {
	sc, nd, err := s.scope.resolve(ctx, req.Msg.GetTarget())
	if err != nil {
		return nil, err
	}
	if nd != nil {
		return connect.NewResponse(&yasakuv1.DeleteTransactionResponse{Needs: nd}), nil
	}
	id, err := uuid.Parse(strings.TrimSpace(req.Msg.GetId()))
	if err != nil {
		return nil, invalidArgCause("id", "id must be a transaction id", err)
	}
	stored, err := s.txs.ByID(sc.ctx, id)
	if err != nil {
		return nil, err
	}
	row := newRefCache(s.wallets, s.cats, s.periods).tx(sc.ctx, stored)
	if !req.Msg.GetConfirm() {
		return connect.NewResponse(&yasakuv1.DeleteTransactionResponse{Preview: row}), nil
	}
	if err := s.txs.Delete(sc.ctx, id); err != nil {
		return nil, err
	}
	return connect.NewResponse(&yasakuv1.DeleteTransactionResponse{Result: row}), nil
}

// ListTransactions returns one filtered page, newest first.
func (s *TransactionService) ListTransactions(ctx context.Context, req *connect.Request[yasakuv1.ListTransactionsRequest]) (*connect.Response[yasakuv1.ListTransactionsResponse], error) {
	sc, nd, err := s.scope.resolve(ctx, req.Msg.GetTarget())
	if err != nil {
		return nil, err
	}
	if nd != nil {
		return nil, needsErr(nd)
	}
	opts := transaction.ListOpts{Limit: int(req.Msg.GetLimit())}
	if q := strings.TrimSpace(req.Msg.GetWallet()); q != "" {
		w, wNd, wErr := resolveWallet(sc.ctx, s.wallets, "wallet", q)
		if wErr != nil {
			return nil, wErr
		}
		if wNd != nil {
			return nil, needsErr(wNd)
		}
		opts.WalletID = &w.ID
	}
	if q := strings.TrimSpace(req.Msg.GetCategory()); q != "" {
		c, cNd, cErr := resolveCategoryAnyKind(sc.ctx, s.cats, "category", q)
		if cErr != nil {
			return nil, cErr
		}
		if cNd != nil {
			return nil, needsErr(cNd)
		}
		opts.CategoryID = &c.ID
	}
	if q := strings.TrimSpace(req.Msg.GetPeriod()); q != "" {
		p, pNd, pErr := resolvePeriod(sc.ctx, s.periods, q)
		if pErr != nil {
			return nil, pErr
		}
		if pNd != nil {
			return nil, needsErr(pNd)
		}
		opts.PeriodID = &p.ID
	}
	if q := strings.TrimSpace(req.Msg.GetKind()); q != "" {
		kind, kErr := transaction.ParseKind(q)
		if kErr != nil {
			return nil, kErr
		}
		opts.Kinds = []transaction.Kind{kind}
	}
	if err := applyCursor(&opts, req.Msg.GetCursor()); err != nil {
		return nil, err
	}
	rows, cursor, err := s.txs.List(sc.ctx, opts)
	if err != nil {
		return nil, err
	}
	refs := newRefCache(s.wallets, s.cats, s.periods)
	in, out := pageTotals(rows)
	return connect.NewResponse(&yasakuv1.ListTransactionsResponse{
		Transactions: refs.txs(sc.ctx, rows),
		NextCursor:   encodeCursor(cursor),
		TotalIn:      in,
		TotalOut:     out,
	}), nil
}

// SearchTransactions matches a note case-insensitively inside an optional inclusive date range.
func (s *TransactionService) SearchTransactions(ctx context.Context, req *connect.Request[yasakuv1.SearchTransactionsRequest]) (*connect.Response[yasakuv1.SearchTransactionsResponse], error) {
	sc, nd, err := s.scope.resolve(ctx, req.Msg.GetTarget())
	if err != nil {
		return nil, err
	}
	if nd != nil {
		return nil, needsErr(nd)
	}
	loc, err := s.location(sc.ctx)
	if err != nil {
		return nil, err
	}
	opts := transaction.ListOpts{
		Search: strings.TrimSpace(req.Msg.GetQuery()),
		Limit:  int(req.Msg.GetLimit()),
	}
	if raw := strings.TrimSpace(req.Msg.GetFrom()); raw != "" {
		d, pErr := parseDate("from", raw)
		if pErr != nil {
			return nil, pErr
		}
		from := d.In(loc)
		opts.From = &from
	}
	if raw := strings.TrimSpace(req.Msg.GetTo()); raw != "" {
		d, pErr := parseDate("to", raw)
		if pErr != nil {
			return nil, pErr
		}
		to := d.AddDays(1).In(loc).Add(-time.Nanosecond)
		opts.To = &to
	}
	if err := applyCursor(&opts, req.Msg.GetCursor()); err != nil {
		return nil, err
	}
	rows, cursor, err := s.txs.List(sc.ctx, opts)
	if err != nil {
		return nil, err
	}
	refs := newRefCache(s.wallets, s.cats, s.periods)
	in, out := pageTotals(rows)
	return connect.NewResponse(&yasakuv1.SearchTransactionsResponse{
		Transactions: refs.txs(sc.ctx, rows),
		NextCursor:   encodeCursor(cursor),
		TotalIn:      in,
		TotalOut:     out,
	}), nil
}

// SuggestCategory reports the category last used for a note like this one.
func (s *TransactionService) SuggestCategory(ctx context.Context, req *connect.Request[yasakuv1.SuggestCategoryRequest]) (*connect.Response[yasakuv1.SuggestCategoryResponse], error) {
	sc, nd, err := s.scope.resolve(ctx, req.Msg.GetTarget())
	if err != nil {
		return nil, err
	}
	if nd != nil {
		return nil, needsErr(nd)
	}
	id, found, err := s.txs.SuggestCategory(sc.ctx, req.Msg.GetNote())
	if err != nil || !found {
		return connect.NewResponse(&yasakuv1.SuggestCategoryResponse{}), err
	}
	c, cErr := s.cats.ByID(sc.ctx, id)
	if cErr != nil {
		// NOTE: the suggestion points at a row that has since gone; that is "no suggestion", not a failure.
		if category.IsNotFoundError(cErr) {
			return connect.NewResponse(&yasakuv1.SuggestCategoryResponse{}), nil
		}
		return nil, cErr
	}
	return connect.NewResponse(&yasakuv1.SuggestCategoryResponse{
		Category: toProtoCategory(c),
		Found:    true,
	}), nil
}

func (s *TransactionService) plan(ctx context.Context, t *yasakuv1.Target, spec recordSpec) (scopeResult, *recordPlan, *yasakuv1.Needs, error) {
	sc, nd, err := s.scope.resolve(ctx, t)
	if err != nil || nd != nil {
		return scopeResult{}, nil, nd, err
	}
	plan, nd, err := s.planIn(sc, spec)
	return sc, plan, nd, err
}

// planIn resolves every reference a write names and builds the preview from the resolved values.
// NOTE: no domain service offers a dry run, so the preview is constructed here and never saved.
func (s *TransactionService) planIn(sc scopeResult, spec recordSpec) (*recordPlan, *yasakuv1.Needs, error) {
	loc, err := s.location(sc.ctx)
	if err != nil {
		return nil, nil, err
	}
	at, err := parseWhen("occurred_at", spec.occurredAt, loc, s.now())
	if err != nil {
		return nil, nil, err
	}
	w, nd, err := resolveWallet(sc.ctx, s.wallets, walletField(spec.kind), spec.wallet)
	if err != nil || nd != nil {
		return nil, nd, err
	}
	in := transaction.RecordInput{
		WalletID:   w.ID,
		Kind:       spec.kind,
		Note:       strings.TrimSpace(spec.note),
		OccurredAt: at,
	}
	out := &yasakuv1.Transaction{
		Kind:       string(spec.kind),
		Wallet:     toWalletRef(w),
		Note:       in.Note,
		OccurredAt: toTimestamp(at),
	}

	if spec.kind == transaction.KindTransfer {
		to, tNd, tErr := resolveWallet(sc.ctx, s.wallets, "to_wallet", spec.toWallet)
		if tErr != nil || tNd != nil {
			return nil, tNd, tErr
		}
		in.ToWalletID = &to.ID
		out.ToWallet = toWalletRef(to)
	}

	amount, err := parseMoney(spec.amount, w.Currency)
	if err != nil {
		return nil, nil, err
	}
	in.Amount = amount
	out.Amount = toMoney(amount)

	var warning string
	if spec.kind.AllowsCategory() {
		kind, kErr := categoryKindFor(spec.kind)
		if kErr != nil {
			return nil, nil, kErr
		}
		if strings.TrimSpace(spec.categoryQ) != "" {
			c, cNd, cErr := resolveCategory(sc.ctx, s.cats, kind, "category", spec.categoryQ)
			if cErr != nil || cNd != nil {
				return nil, cNd, cErr
			}
			in.CategoryID = &c.ID
			out.Category = toProtoCategory(c)
		} else {
			warning = s.suggestionWarning(sc.ctx, in.Note)
		}
	}

	p, nd, err := s.resolveWritePeriod(sc.ctx, spec.periodQ, at, loc)
	if err != nil || nd != nil {
		return nil, nd, err
	}
	if p != nil {
		if strings.TrimSpace(spec.periodQ) != "" {
			in.PeriodID = &p.ID
		}
		out.PeriodId, out.PeriodName = p.ID.String(), p.Name
	} else {
		warning = joinWarnings(warning, "no period covers that date yet; recording will open the first one")
	}
	return &recordPlan{input: in, preview: out, warning: warning}, nil, nil
}

// resolveWritePeriod names the period a write would land in without creating one.
func (s *TransactionService) resolveWritePeriod(ctx context.Context, q string, at time.Time, loc *time.Location) (*period.Period, *yasakuv1.Needs, error) {
	if strings.TrimSpace(q) != "" {
		return resolvePeriod(ctx, s.periods, q)
	}
	p, err := periodContaining(ctx, s.periods, at, loc)
	return p, nil, err
}

// suggestionWarning is advisory only: a commit uses the category the request names, never the suggestion.
func (s *TransactionService) suggestionWarning(ctx context.Context, note string) string {
	if strings.TrimSpace(note) == "" {
		return ""
	}
	id, found, err := s.txs.SuggestCategory(ctx, note)
	if err != nil || !found {
		return ""
	}
	c, err := s.cats.ByID(ctx, id)
	if err != nil {
		return ""
	}
	return "category suggested: " + c.Name + " (pass it back as category to apply it)"
}

func (s *TransactionService) commit(sc scopeResult, plan *recordPlan) (*yasakuv1.Transaction, error) {
	t, err := s.txs.Record(sc.ctx, plan.input)
	if err != nil {
		return nil, err
	}
	refs := newRefCache(s.wallets, s.cats, s.periods)
	return refs.tx(sc.ctx, t), nil
}

func (s *TransactionService) location(ctx context.Context) (*time.Location, error) {
	settings, err := s.ledgers.Get(ctx)
	if err != nil {
		return nil, err
	}
	return settings.Location()
}

func respondReviseNeeds(nd *yasakuv1.Needs, err error) (*connect.Response[yasakuv1.ReviseTransactionResponse], error) {
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&yasakuv1.ReviseTransactionResponse{Needs: nd}), nil
}

func walletField(kind transaction.Kind) string {
	if kind == transaction.KindTransfer {
		return "from_wallet"
	}
	return "wallet"
}

func categoryKindFor(kind transaction.Kind) (category.Kind, error) {
	switch kind {
	case transaction.KindExpense:
		return category.KindExpense, nil
	case transaction.KindIncome:
		return category.KindIncome, nil
	default:
		return "", invalidArg("category", "a "+string(kind)+" transaction carries no category")
	}
}

func batchKind(raw string) (transaction.Kind, error) {
	kind, err := transaction.ParseKind(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	switch kind {
	case transaction.KindIncome, transaction.KindExpense, transaction.KindTransfer:
		return kind, nil
	default:
		return "", invalidArg("kind", "a batch may only record income, expense or transfer")
	}
}

func failedOutcome(i int, err error) *yasakuv1.BatchOutcome {
	out := &yasakuv1.BatchOutcome{
		Index: int32(i), //nolint:gosec // bounded by len(items).
		Error: err.Error(),
	}
	if ae, ok := apperror.AsAppError(err); ok {
		out.ErrorCode = ae.Code()
		out.Error = ae.Message()
	}
	return out
}

func needsOutcome(i int, nd *yasakuv1.Needs) *yasakuv1.BatchOutcome {
	return failedOutcome(i, needsErr(nd))
}

func applyCursor(opts *transaction.ListOpts, raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	c, err := transaction.ParseCursor(raw)
	if err != nil {
		return invalidArgCause("cursor", "the cursor is not one this server issued", err)
	}
	opts.After = &c
	return nil
}

func encodeCursor(c *transaction.Cursor) string {
	if c == nil {
		return ""
	}
	return c.Encode()
}

func joinWarnings(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + "; " + b
}
