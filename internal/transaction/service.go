package transaction

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/money"
)

//nolint:gochecknoglobals // OTel tracer is a package-level fixture, not runtime state.
var tracer = otel.Tracer("altalune.id/yasaku/internal/transaction")

// Service is the transactions driving port.
type Service struct {
	store      Store
	log        *slog.Logger
	unexpected apperror.UnexpectedFunc

	wallets    WalletReader
	categories CategoryReader
	periods    PeriodResolver
	uow        UnitOfWork
	suggester  Suggester
}

// NewService binds the service to its dependencies.
func NewService(
	store Store,
	log *slog.Logger,
	unexpected apperror.UnexpectedFunc,
	wallets WalletReader,
	categories CategoryReader,
	periods PeriodResolver,
	uow UnitOfWork,
) *Service {
	return &Service{
		store:      store,
		log:        log.With("module", "transaction"),
		unexpected: unexpected,
		wallets:    wallets,
		categories: categories,
		periods:    periods,
		uow:        uow,
		suggester:  lastUsedSuggester{store: store},
	}
}

// RecordInput carries a new transaction as the caller describes it.
type RecordInput struct {
	WalletID   uuid.UUID
	ToWalletID *uuid.UUID
	Kind       Kind
	Amount     money.Amount
	CategoryID *uuid.UUID
	PeriodID   *uuid.UUID
	Note       string
	OccurredAt time.Time
}

// RevisePatch carries the fields a caller wants changed; a nil field is left alone and a nil inner pointer clears the column.
type RevisePatch struct {
	WalletID   *uuid.UUID
	ToWalletID **uuid.UUID
	Amount     *money.Amount
	CategoryID **uuid.UUID
	PeriodID   **uuid.UUID
	Note       *string
	OccurredAt *time.Time
}

// BatchResult is one row's outcome in a RecordBatch, keyed back to its input index.
type BatchResult struct {
	Index       int
	Transaction *Transaction
	Err         error
}

// Record validates and persists one transaction in the caller's tenant scope.
func (s *Service) Record(ctx context.Context, in RecordInput) (*Transaction, error) {
	ctx, span := tracer.Start(ctx, "transaction.Record")
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	span.SetAttributes(
		attribute.String("org_id", tc.OrgID.String()),
		attribute.String("project_id", tc.ProjectID.String()),
		attribute.String("transaction.kind", string(in.Kind)),
	)
	return s.record(ctx, tc, in, tc.UserID)
}

// RecordBatch records every input independently; one bad row does not roll back the others.
func (s *Service) RecordBatch(ctx context.Context, in []RecordInput) []BatchResult {
	ctx, span := tracer.Start(ctx, "transaction.RecordBatch",
		trace.WithAttributes(attribute.Int("transaction.batch_size", len(in))))
	defer span.End()

	out := make([]BatchResult, 0, len(in))
	for i, row := range in {
		t, err := s.Record(ctx, row)
		if err != nil {
			out = append(out, BatchResult{Index: i, Err: err})
			continue
		}
		out = append(out, BatchResult{Index: i, Transaction: t})
	}
	return out
}

func (s *Service) record(ctx context.Context, tc tenant.Context, in RecordInput, by uuid.UUID) (*Transaction, error) {
	currency, err := s.checkWallets(ctx, tc, in.WalletID, in.ToWalletID, in.Kind, in.Amount)
	if err != nil {
		return nil, err
	}
	if err := s.checkCategory(ctx, tc, in.CategoryID, in.Kind); err != nil {
		return nil, err
	}
	periodID, err := s.resolvePeriod(ctx, tc, in.OccurredAt, in.PeriodID)
	if err != nil {
		return nil, err
	}

	t, err := New(tc.OrgID, tc.ProjectID, NewParams{
		WalletID:   in.WalletID,
		ToWalletID: in.ToWalletID,
		Kind:       in.Kind,
		Amount:     money.New(in.Amount.Minor, currency),
		CategoryID: in.CategoryID,
		PeriodID:   periodID,
		Note:       in.Note,
		OccurredAt: in.OccurredAt,
		CreatedBy:  by,
	})
	if err != nil {
		return nil, err
	}
	if err := s.store.Save(ctx, t); err != nil {
		if IsNotFoundError(err) {
			return nil, err
		}
		return nil, s.unexpected(ctx, "transaction.Record: save", err,
			"org_id", tc.OrgID, "project_id", tc.ProjectID, "wallet_id", in.WalletID)
	}
	return t, nil
}

// Revise applies p to the identified transaction when it is in the caller's tenant scope.
func (s *Service) Revise(ctx context.Context, id uuid.UUID, p RevisePatch) (*Transaction, error) {
	ctx, span := tracer.Start(ctx, "transaction.Revise",
		trace.WithAttributes(attribute.String("transaction.id", id.String())))
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	t, err := s.load(ctx, tc, id, "transaction.Revise")
	if err != nil {
		return nil, err
	}

	if err := s.refusePeriodLocked(ctx, tc, t.PeriodID); err != nil {
		return nil, err
	}

	next := *t
	movedInTime := false
	if p.OccurredAt != nil {
		next.SetOccurredAt(*p.OccurredAt)
		movedInTime = !next.OccurredAt.Equal(t.OccurredAt)
	}
	if p.WalletID != nil {
		next.SetWallet(*p.WalletID)
	}
	if p.ToWalletID != nil {
		next.SetToWallet(*p.ToWalletID)
	}
	if p.Note != nil {
		if err := next.SetNote(*p.Note); err != nil {
			return nil, err
		}
	}
	if p.Amount != nil {
		if err := next.SetAmount(*p.Amount); err != nil {
			return nil, err
		}
	}
	if p.CategoryID != nil {
		if err := next.SetCategory(*p.CategoryID); err != nil {
			return nil, err
		}
	}
	if err := next.validate(); err != nil {
		return nil, err
	}

	currency, err := s.checkWallets(ctx, tc, next.WalletID, next.ToWalletID, next.Kind, next.Amount)
	if err != nil {
		return nil, err
	}
	next.Amount = money.New(next.Amount.Minor, currency)
	if err := s.checkCategory(ctx, tc, next.CategoryID, next.Kind); err != nil {
		return nil, err
	}

	switch {
	case p.PeriodID != nil && *p.PeriodID == nil:
		next.SetPeriod(nil)
	case p.PeriodID != nil:
		periodID, pErr := s.resolvePeriod(ctx, tc, next.OccurredAt, *p.PeriodID)
		if pErr != nil {
			return nil, pErr
		}
		next.SetPeriod(periodID)
	case movedInTime:
		periodID, pErr := s.resolvePeriod(ctx, tc, next.OccurredAt, nil)
		if pErr != nil {
			return nil, pErr
		}
		next.SetPeriod(periodID)
	}

	if err := s.store.Save(ctx, &next); err != nil {
		if IsNotFoundError(err) {
			return nil, err
		}
		return nil, s.unexpected(ctx, "transaction.Revise: save", err, "transaction_id", id)
	}
	return &next, nil
}

// Delete removes the identified transaction when it is in the caller's tenant scope and its period is open.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	ctx, span := tracer.Start(ctx, "transaction.Delete",
		trace.WithAttributes(attribute.String("transaction.id", id.String())))
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return err
	}
	t, err := s.load(ctx, tc, id, "transaction.Delete")
	if err != nil {
		return err
	}
	if err := s.refusePeriodLocked(ctx, tc, t.PeriodID); err != nil {
		return err
	}
	if err := s.store.Delete(ctx, id); err != nil {
		if IsNotFoundError(err) {
			return err
		}
		return s.unexpected(ctx, "transaction.Delete: delete", err, "transaction_id", id)
	}
	return nil
}

// ByID returns the identified transaction when it belongs to the caller's tenant scope.
func (s *Service) ByID(ctx context.Context, id uuid.UUID) (*Transaction, error) {
	ctx, span := tracer.Start(ctx, "transaction.ByID",
		trace.WithAttributes(attribute.String("transaction.id", id.String())))
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	return s.load(ctx, tc, id, "transaction.ByID")
}

// List returns one page of transactions newest-first, plus the cursor for the next page when the page is full.
func (s *Service) List(ctx context.Context, opts ListOpts) ([]*Transaction, *Cursor, error) {
	ctx, span := tracer.Start(ctx, "transaction.List")
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, nil, err
	}
	limit := opts.NormalizedLimit()
	opts.Limit = limit

	items, err := s.store.List(ctx, tc.OrgID, tc.ProjectID, opts)
	if err != nil {
		return nil, nil, s.unexpected(ctx, "transaction.List: list", err,
			"org_id", tc.OrgID, "project_id", tc.ProjectID)
	}
	if len(items) < limit {
		return items, nil, nil
	}
	last := items[len(items)-1]
	return items, &Cursor{OccurredAt: last.OccurredAt, CreatedAt: last.CreatedAt, ID: last.ID}, nil
}

// Balances returns the derived balance of each wallet in the caller's project that has at least one transaction.
// NOTE: an untouched wallet is absent rather than zero, so callers merge the map against their own wallet list instead of indexing it blind.
func (s *Service) Balances(ctx context.Context) (map[uuid.UUID]money.Amount, error) {
	ctx, span := tracer.Start(ctx, "transaction.Balances")
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	out, err := s.store.Balances(ctx, tc.OrgID, tc.ProjectID)
	if err != nil {
		return nil, s.unexpected(ctx, "transaction.Balances: balances", err,
			"org_id", tc.OrgID, "project_id", tc.ProjectID)
	}
	return out, nil
}

// Balance returns one wallet's derived balance.
func (s *Service) Balance(ctx context.Context, walletID uuid.UUID) (money.Amount, error) {
	ctx, span := tracer.Start(ctx, "transaction.Balance",
		trace.WithAttributes(attribute.String("wallet.id", walletID.String())))
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return money.Amount{}, err
	}
	out, err := s.store.Balance(ctx, tc.OrgID, tc.ProjectID, walletID)
	if err != nil {
		return money.Amount{}, s.unexpected(ctx, "transaction.Balance: balance", err, "wallet_id", walletID)
	}
	if out.Currency == "" {
		// NOTE: a currency-less zero panics on money.Add, so an untouched wallet borrows its currency.
		w, wErr := s.wallet(ctx, tc, walletID)
		if wErr != nil {
			return money.Amount{}, wErr
		}
		out = money.Zero(w.Currency)
	}
	return out, nil
}

// SuggestCategory proposes the category last used for an equivalent note.
func (s *Service) SuggestCategory(ctx context.Context, note string) (uuid.UUID, bool, error) {
	ctx, span := tracer.Start(ctx, "transaction.SuggestCategory")
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return uuid.Nil, false, err
	}
	id, ok, err := s.suggester.Suggest(ctx, tc.OrgID, tc.ProjectID, note)
	if err != nil {
		return uuid.Nil, false, s.unexpected(ctx, "transaction.SuggestCategory: suggest", err,
			"org_id", tc.OrgID, "project_id", tc.ProjectID)
	}
	return id, ok, nil
}

// RecordOpening writes a wallet's starting balance on behalf of by, which need not be the ctx user.
func (s *Service) RecordOpening(ctx context.Context, walletID uuid.UUID, amount money.Amount, at time.Time, by uuid.UUID) error {
	ctx, span := tracer.Start(ctx, "transaction.RecordOpening",
		trace.WithAttributes(attribute.String("wallet.id", walletID.String())))
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return err
	}
	_, err = s.record(ctx, tc, RecordInput{
		WalletID:   walletID,
		Kind:       KindOpening,
		Amount:     amount,
		OccurredAt: at,
	}, by)
	return err
}

// Adjust writes the adjustment that brings walletID's derived balance to target, or nothing when it already matches.
// NOTE: the read and the write need a real UnitOfWork and are serialized only against other Adjust calls on the same wallet; a concurrent Record, Revise or Delete takes no lock and can still land between them, so the final balance is not guaranteed to equal target.
func (s *Service) Adjust(ctx context.Context, walletID uuid.UUID, target money.Amount, at time.Time, by uuid.UUID) (*Transaction, error) {
	ctx, span := tracer.Start(ctx, "transaction.Adjust",
		trace.WithAttributes(attribute.String("wallet.id", walletID.String())))
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	w, err := s.wallet(ctx, tc, walletID)
	if err != nil {
		return nil, err
	}
	if w.Archived {
		return nil, &WalletArchivedError{WalletID: walletID.String()}
	}
	if target.Currency != w.Currency {
		return nil, &CurrencyMismatchError{
			WalletID:       walletID.String(),
			WalletCurrency: string(w.Currency),
			AmountCurrency: string(target.Currency),
		}
	}

	var out *Transaction
	err = s.uow(ctx, func(ctx context.Context) error {
		if lErr := s.store.LockWallet(ctx, tc.OrgID, tc.ProjectID, walletID); lErr != nil {
			return s.unexpected(ctx, "transaction.Adjust: lock wallet", lErr, "wallet_id", walletID)
		}
		current, bErr := s.store.Balance(ctx, tc.OrgID, tc.ProjectID, walletID)
		if bErr != nil {
			return s.unexpected(ctx, "transaction.Adjust: balance", bErr, "wallet_id", walletID)
		}
		if current.Currency != "" && current.Currency != w.Currency {
			return &CurrencyMismatchError{
				WalletID:       walletID.String(),
				WalletCurrency: string(w.Currency),
				AmountCurrency: string(current.Currency),
			}
		}
		delta := target.Minor - current.Minor
		if delta == 0 {
			return nil
		}
		kind := KindAdjustmentIn
		if delta < 0 {
			kind, delta = KindAdjustmentOut, -delta
		}
		t, rErr := s.record(ctx, tc, RecordInput{
			WalletID:   walletID,
			Kind:       kind,
			Amount:     money.New(delta, w.Currency),
			OccurredAt: at,
		}, by)
		if rErr != nil {
			return rErr
		}
		out = t
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) load(ctx context.Context, tc tenant.Context, id uuid.UUID, op string) (*Transaction, error) {
	t, err := s.store.ByID(ctx, id)
	if err != nil {
		if IsNotFoundError(err) {
			return nil, err
		}
		return nil, s.unexpected(ctx, op+": byID", err, "transaction_id", id)
	}
	// SECURITY: the store filters by org, not project; without this a sibling project's row would be reachable by id.
	if t.OrgID != tc.OrgID || t.ProjectID != tc.ProjectID {
		return nil, &NotFoundError{ID: id.String()}
	}
	return t, nil
}

func (s *Service) wallet(ctx context.Context, tc tenant.Context, id uuid.UUID) (WalletInfo, error) {
	w, err := s.wallets.Wallet(ctx, tc.OrgID, tc.ProjectID, id)
	if err != nil {
		if _, ok := apperror.AsAppError(err); ok {
			return WalletInfo{}, err
		}
		return WalletInfo{}, s.unexpected(ctx, "transaction: wallet lookup", err, "wallet_id", id)
	}
	return w, nil
}

func (s *Service) checkWallets(ctx context.Context, tc tenant.Context, walletID uuid.UUID, toWalletID *uuid.UUID, kind Kind, amount money.Amount) (money.Currency, error) {
	w, err := s.wallet(ctx, tc, walletID)
	if err != nil {
		return "", err
	}
	if w.Archived {
		return "", &WalletArchivedError{WalletID: walletID.String()}
	}
	if amount.Currency != w.Currency {
		return "", &CurrencyMismatchError{
			WalletID:       walletID.String(),
			WalletCurrency: string(w.Currency),
			AmountCurrency: string(amount.Currency),
		}
	}
	if kind != KindTransfer || toWalletID == nil {
		return w.Currency, nil
	}
	to, err := s.wallet(ctx, tc, *toWalletID)
	if err != nil {
		return "", err
	}
	if to.Archived {
		return "", &WalletArchivedError{WalletID: toWalletID.String()}
	}
	if to.Currency != w.Currency {
		return "", &CurrencyMismatchError{
			WalletID:       toWalletID.String(),
			WalletCurrency: string(to.Currency),
			AmountCurrency: string(w.Currency),
		}
	}
	return w.Currency, nil
}

func (s *Service) checkCategory(ctx context.Context, tc tenant.Context, categoryID *uuid.UUID, kind Kind) error {
	if categoryID == nil {
		return nil
	}
	if !kind.AllowsCategory() {
		return &InvalidKindError{Kind: string(kind), Reason: "only income and expense may name a category"}
	}
	c, err := s.categories.Category(ctx, tc.OrgID, tc.ProjectID, *categoryID)
	if err != nil {
		if _, ok := apperror.AsAppError(err); ok {
			return err
		}
		return s.unexpected(ctx, "transaction: category lookup", err, "category_id", *categoryID)
	}
	if c.Kind != string(kind) {
		return &CategoryKindMismatchError{
			CategoryID:      categoryID.String(),
			CategoryKind:    c.Kind,
			TransactionKind: string(kind),
		}
	}
	return nil
}

func (s *Service) resolvePeriod(ctx context.Context, tc tenant.Context, at time.Time, want *uuid.UUID) (*uuid.UUID, error) {
	info, ok, err := s.periods.Containing(ctx, tc.OrgID, tc.ProjectID, at)
	if err != nil {
		return nil, s.periodErr(ctx, "transaction: containing period", err)
	}

	chosen := want
	if want == nil {
		if !ok {
			return nil, nil
		}
		id := info.ID
		chosen = &id
	} else if !ok || !adjacent(info, *want) {
		return nil, &PeriodNotAdjacentError{PeriodID: want.String()}
	}

	target, err := s.periods.ByID(ctx, tc.OrgID, tc.ProjectID, *chosen)
	if err != nil {
		return nil, s.periodErr(ctx, "transaction: period lookup", err)
	}
	if target.Locked {
		return nil, &PeriodLockedError{PeriodID: chosen.String()}
	}
	return chosen, nil
}

func (s *Service) refusePeriodLocked(ctx context.Context, tc tenant.Context, periodID *uuid.UUID) error {
	if periodID == nil {
		return nil
	}
	info, err := s.periods.ByID(ctx, tc.OrgID, tc.ProjectID, *periodID)
	if err != nil {
		return s.periodErr(ctx, "transaction: period lookup", err)
	}
	if info.Locked {
		return &PeriodLockedError{PeriodID: periodID.String()}
	}
	return nil
}

func (s *Service) periodErr(ctx context.Context, op string, err error) error {
	if _, ok := apperror.AsAppError(err); ok {
		return err
	}
	return s.unexpected(ctx, op, err)
}

func adjacent(info PeriodInfo, id uuid.UUID) bool {
	if id == info.ID {
		return true
	}
	if info.PrevID != nil && id == *info.PrevID {
		return true
	}
	return info.NextID != nil && id == *info.NextID
}
