package transaction_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	apperrorv1 "altalune.id/yasaku/gen/go/apperror/v1"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/money"
)

type periodStub struct {
	infos     map[uuid.UUID]transaction.PeriodInfo
	byInstant func(at time.Time) (uuid.UUID, bool)
}

func (p *periodStub) Containing(_ context.Context, _, _ uuid.UUID, at time.Time) (transaction.PeriodInfo, bool, error) {
	id, ok := p.byInstant(at)
	if !ok {
		return transaction.PeriodInfo{}, false, nil
	}
	return p.infos[id], true, nil
}

func (p *periodStub) ByID(_ context.Context, _, _, id uuid.UUID) (transaction.PeriodInfo, error) {
	info, ok := p.infos[id]
	if !ok {
		return transaction.PeriodInfo{}, fmt.Errorf("period %s: not found", id)
	}
	return info, nil
}

type fixture struct {
	svc        *transaction.Service
	store      *fakes.Transaction
	ctx        context.Context
	tc         tenant.Context
	wallets    map[uuid.UUID]transaction.WalletInfo
	categories map[uuid.UUID]transaction.CategoryInfo
	periods    *periodStub

	walletIDR   uuid.UUID
	walletIDR2  uuid.UUID
	walletUSD   uuid.UUID
	archived    uuid.UUID
	expenseCat  uuid.UUID
	incomeCat   uuid.UUID
	july        uuid.UUID
	august      uuid.UUID
	september   uuid.UUID
	unexpecteds *int
	uowEntries  *int
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	f := &fixture{
		store:      fakes.NewTransaction(),
		wallets:    map[uuid.UUID]transaction.WalletInfo{},
		categories: map[uuid.UUID]transaction.CategoryInfo{},
		tc:         tenant.Context{OrgID: uuid.New(), ProjectID: uuid.New(), UserID: uuid.New()},
	}
	f.ctx = tenant.Into(context.Background(), f.tc)

	f.walletIDR, f.walletIDR2, f.walletUSD, f.archived = uuid.New(), uuid.New(), uuid.New(), uuid.New()
	f.wallets[f.walletIDR] = transaction.WalletInfo{ID: f.walletIDR, Currency: money.IDR}
	f.wallets[f.walletIDR2] = transaction.WalletInfo{ID: f.walletIDR2, Currency: money.IDR}
	f.wallets[f.walletUSD] = transaction.WalletInfo{ID: f.walletUSD, Currency: money.Currency("USD")}
	f.wallets[f.archived] = transaction.WalletInfo{ID: f.archived, Currency: money.IDR, Archived: true}

	f.expenseCat, f.incomeCat = uuid.New(), uuid.New()
	f.categories[f.expenseCat] = transaction.CategoryInfo{ID: f.expenseCat, Kind: "expense"}
	f.categories[f.incomeCat] = transaction.CategoryInfo{ID: f.incomeCat, Kind: "income"}

	f.july, f.august, f.september = uuid.New(), uuid.New(), uuid.New()
	f.periods = &periodStub{
		infos: map[uuid.UUID]transaction.PeriodInfo{
			f.july:      {ID: f.july, NextID: ptr(f.august)},
			f.august:    {ID: f.august, PrevID: ptr(f.july), NextID: ptr(f.september)},
			f.september: {ID: f.september, PrevID: ptr(f.august)},
		},
		byInstant: func(at time.Time) (uuid.UUID, bool) {
			switch at.UTC().Month() {
			case time.July:
				return f.july, true
			case time.August:
				return f.august, true
			case time.September:
				return f.september, true
			}
			return uuid.Nil, false
		},
	}

	calls := 0
	f.unexpecteds = &calls
	unexpected := func(_ context.Context, _ string, err error, _ ...any) *apperror.AppError {
		calls++
		return apperror.New("yasaku.unexpected", err.Error(), codes.Internal,
			&apperrorv1.ErrorDetail{Code: "yasaku.unexpected"}).WithCause(err)
	}

	wallets := transaction.WalletReaderFunc(func(_ context.Context, _, _, id uuid.UUID) (transaction.WalletInfo, error) {
		w, ok := f.wallets[id]
		if !ok {
			return transaction.WalletInfo{}, &transaction.NotFoundError{ID: id.String()}
		}
		return w, nil
	})
	categories := transaction.CategoryReaderFunc(func(_ context.Context, _, _, id uuid.UUID) (transaction.CategoryInfo, error) {
		c, ok := f.categories[id]
		if !ok {
			return transaction.CategoryInfo{}, &transaction.NotFoundError{ID: id.String()}
		}
		return c, nil
	})
	entries := 0
	f.uowEntries = &entries
	uow := transaction.UnitOfWork(func(ctx context.Context, fn func(ctx context.Context) error) error {
		entries++
		return fn(ctx)
	})

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	f.svc = transaction.NewService(f.store, log, unexpected, wallets, categories, f.periods, uow)
	return f
}

func ptr[T any](v T) *T { return &v }

func augustDay(day int) time.Time {
	return time.Date(2026, 8, day, 12, 0, 0, 0, time.UTC)
}

func (f *fixture) expense(t *testing.T, at time.Time, amount int64, note string) *transaction.Transaction {
	t.Helper()
	got, err := f.svc.Record(f.ctx, transaction.RecordInput{
		WalletID:   f.walletIDR,
		Kind:       transaction.KindExpense,
		Amount:     idr(amount),
		CategoryID: &f.expenseCat,
		Note:       note,
		OccurredAt: at,
	})
	if err != nil {
		t.Fatalf("seed Record: %v", err)
	}
	return got
}

func TestService_Record(t *testing.T) {
	t.Run("assigns the containing period", func(t *testing.T) {
		f := newFixture(t)
		got := f.expense(t, augustDay(10), 25_000, "Kopi")
		if got.PeriodID == nil || *got.PeriodID != f.august {
			t.Fatalf("period=%v want august", got.PeriodID)
		}
		if got.CreatedBy != f.tc.UserID {
			t.Errorf("CreatedBy=%v want the ctx user", got.CreatedBy)
		}
		if *f.unexpecteds != 0 {
			t.Errorf("unexpected() called %d times", *f.unexpecteds)
		}
	})

	t.Run("leaves the period nil when no period covers the instant", func(t *testing.T) {
		f := newFixture(t)
		got, err := f.svc.Record(f.ctx, transaction.RecordInput{
			WalletID:   f.walletIDR,
			Kind:       transaction.KindExpense,
			Amount:     idr(1_000),
			OccurredAt: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
		})
		if err != nil {
			t.Fatalf("Record: %v", err)
		}
		if got.PeriodID != nil {
			t.Errorf("period=%v want nil", got.PeriodID)
		}
	})

	t.Run("accepts the previous period as payday carryover", func(t *testing.T) {
		f := newFixture(t)
		got, err := f.svc.Record(f.ctx, transaction.RecordInput{
			WalletID:   f.walletIDR,
			Kind:       transaction.KindIncome,
			Amount:     idr(9_000_000),
			CategoryID: &f.incomeCat,
			PeriodID:   &f.july,
			Note:       "Salary",
			OccurredAt: augustDay(1),
		})
		if err != nil {
			t.Fatalf("Record: %v", err)
		}
		if got.PeriodID == nil || *got.PeriodID != f.july {
			t.Errorf("period=%v want july", got.PeriodID)
		}
	})

	t.Run("refuses a period that is neither containing nor adjacent", func(t *testing.T) {
		f := newFixture(t)
		unrelated := uuid.New()
		f.periods.infos[unrelated] = transaction.PeriodInfo{ID: unrelated}
		_, err := f.svc.Record(f.ctx, transaction.RecordInput{
			WalletID:   f.walletIDR,
			Kind:       transaction.KindExpense,
			Amount:     idr(1_000),
			PeriodID:   &unrelated,
			OccurredAt: augustDay(10),
		})
		if !transaction.IsPeriodNotAdjacentError(err) {
			t.Errorf("want IsPeriodNotAdjacentError, got %v", err)
		}
	})

	t.Run("refuses a locked period", func(t *testing.T) {
		f := newFixture(t)
		f.periods.infos[f.august] = transaction.PeriodInfo{ID: f.august, Locked: true, PrevID: ptr(f.july), NextID: ptr(f.september)}
		_, err := f.svc.Record(f.ctx, transaction.RecordInput{
			WalletID:   f.walletIDR,
			Kind:       transaction.KindExpense,
			Amount:     idr(1_000),
			OccurredAt: augustDay(10),
		})
		if !transaction.IsPeriodLockedError(err) {
			t.Errorf("want IsPeriodLockedError, got %v", err)
		}
	})

	t.Run("refuses an archived wallet", func(t *testing.T) {
		f := newFixture(t)
		_, err := f.svc.Record(f.ctx, transaction.RecordInput{
			WalletID:   f.archived,
			Kind:       transaction.KindExpense,
			Amount:     idr(1_000),
			OccurredAt: augustDay(10),
		})
		if !transaction.IsWalletArchivedError(err) {
			t.Errorf("want IsWalletArchivedError, got %v", err)
		}
	})

	t.Run("refuses an amount in another currency than the wallet", func(t *testing.T) {
		f := newFixture(t)
		_, err := f.svc.Record(f.ctx, transaction.RecordInput{
			WalletID:   f.walletUSD,
			Kind:       transaction.KindExpense,
			Amount:     idr(1_000),
			OccurredAt: augustDay(10),
		})
		if !transaction.IsCurrencyMismatchError(err) {
			t.Errorf("want IsCurrencyMismatchError, got %v", err)
		}
	})

	t.Run("refuses an expense category on an income", func(t *testing.T) {
		f := newFixture(t)
		_, err := f.svc.Record(f.ctx, transaction.RecordInput{
			WalletID:   f.walletIDR,
			Kind:       transaction.KindIncome,
			Amount:     idr(1_000),
			CategoryID: &f.expenseCat,
			OccurredAt: augustDay(10),
		})
		if !transaction.IsCategoryKindMismatchError(err) {
			t.Errorf("want IsCategoryKindMismatchError, got %v", err)
		}
	})

	t.Run("refuses a transfer whose destination wallet is archived", func(t *testing.T) {
		f := newFixture(t)
		_, err := f.svc.Record(f.ctx, transaction.RecordInput{
			WalletID:   f.walletIDR,
			ToWalletID: &f.archived,
			Kind:       transaction.KindTransfer,
			Amount:     idr(1_000),
			OccurredAt: augustDay(10),
		})
		if !transaction.IsWalletArchivedError(err) {
			t.Errorf("want IsWalletArchivedError, got %v", err)
		}
	})

	t.Run("refuses a transfer across currencies", func(t *testing.T) {
		f := newFixture(t)
		_, err := f.svc.Record(f.ctx, transaction.RecordInput{
			WalletID:   f.walletIDR,
			ToWalletID: &f.walletUSD,
			Kind:       transaction.KindTransfer,
			Amount:     idr(1_000),
			OccurredAt: augustDay(10),
		})
		if !transaction.IsCurrencyMismatchError(err) {
			t.Errorf("want IsCurrencyMismatchError, got %v", err)
		}
	})
}

func TestService_RecordBatch(t *testing.T) {
	f := newFixture(t)
	results := f.svc.RecordBatch(f.ctx, []transaction.RecordInput{
		{WalletID: f.walletIDR, Kind: transaction.KindExpense, Amount: idr(1_000), OccurredAt: augustDay(1)},
		{WalletID: f.archived, Kind: transaction.KindExpense, Amount: idr(2_000), OccurredAt: augustDay(2)},
		{WalletID: f.walletIDR, Kind: transaction.KindExpense, Amount: idr(3_000), OccurredAt: augustDay(3)},
	})
	if len(results) != 3 {
		t.Fatalf("results=%d want 3", len(results))
	}
	for i, r := range results {
		if r.Index != i {
			t.Errorf("results[%d].Index=%d", i, r.Index)
		}
	}
	if results[0].Err != nil || results[0].Transaction == nil {
		t.Errorf("row 0: %v", results[0].Err)
	}
	if !transaction.IsWalletArchivedError(results[1].Err) {
		t.Errorf("row 1: want IsWalletArchivedError, got %v", results[1].Err)
	}
	if results[1].Transaction != nil {
		t.Error("a failed row must carry no transaction")
	}
	if results[2].Err != nil {
		t.Errorf("row 2: %v", results[2].Err)
	}
	got, _ := f.store.Balances(f.ctx, f.tc.OrgID, f.tc.ProjectID)
	if got[f.walletIDR].Minor != -4_000 {
		t.Errorf("balance=%v want -4000; the good rows must survive the bad one", got[f.walletIDR])
	}
}

func TestService_Revise(t *testing.T) {
	t.Run("moving occurred_at into a locked period is refused", func(t *testing.T) {
		f := newFixture(t)
		tx := f.expense(t, augustDay(10), 25_000, "Kopi")
		f.periods.infos[f.september] = transaction.PeriodInfo{ID: f.september, Locked: true, PrevID: ptr(f.august)}

		sept := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
		_, err := f.svc.Revise(f.ctx, tx.ID, transaction.RevisePatch{OccurredAt: &sept})
		if !transaction.IsPeriodLockedError(err) {
			t.Fatalf("want IsPeriodLockedError, got %v", err)
		}
		after, _ := f.store.ByID(f.ctx, tx.ID)
		if !after.OccurredAt.Equal(augustDay(10)) {
			t.Error("a refused Revise must not persist")
		}
	})

	t.Run("a transaction already in a locked period cannot be revised", func(t *testing.T) {
		f := newFixture(t)
		tx := f.expense(t, augustDay(10), 25_000, "Kopi")
		f.periods.infos[f.august] = transaction.PeriodInfo{ID: f.august, Locked: true, PrevID: ptr(f.july), NextID: ptr(f.september)}

		_, err := f.svc.Revise(f.ctx, tx.ID, transaction.RevisePatch{Note: ptr("Kopi Kenangan")})
		if !transaction.IsPeriodLockedError(err) {
			t.Errorf("want IsPeriodLockedError, got %v", err)
		}
	})

	t.Run("changing the note keeps the period", func(t *testing.T) {
		f := newFixture(t)
		tx := f.expense(t, augustDay(10), 25_000, "Kopi")
		got, err := f.svc.Revise(f.ctx, tx.ID, transaction.RevisePatch{Note: ptr("Kopi Kenangan")})
		if err != nil {
			t.Fatalf("Revise: %v", err)
		}
		if got.Note != "Kopi Kenangan" {
			t.Errorf("note=%q", got.Note)
		}
		if got.PeriodID == nil || *got.PeriodID != f.august {
			t.Errorf("period=%v want august unchanged", got.PeriodID)
		}
	})

	t.Run("re-resolves the period when occurred_at moves", func(t *testing.T) {
		f := newFixture(t)
		tx := f.expense(t, augustDay(10), 25_000, "Kopi")
		sept := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
		got, err := f.svc.Revise(f.ctx, tx.ID, transaction.RevisePatch{OccurredAt: &sept})
		if err != nil {
			t.Fatalf("Revise: %v", err)
		}
		if got.PeriodID == nil || *got.PeriodID != f.september {
			t.Errorf("period=%v want september", got.PeriodID)
		}
	})

	t.Run("an explicit period wins over the containing one", func(t *testing.T) {
		f := newFixture(t)
		tx := f.expense(t, augustDay(10), 25_000, "Kopi")
		got, err := f.svc.Revise(f.ctx, tx.ID, transaction.RevisePatch{PeriodID: ptr(&f.september)})
		if err != nil {
			t.Fatalf("Revise: %v", err)
		}
		if got.PeriodID == nil || *got.PeriodID != f.september {
			t.Errorf("period=%v want september", got.PeriodID)
		}
	})

	t.Run("clears the period when the patch names nil", func(t *testing.T) {
		f := newFixture(t)
		tx := f.expense(t, augustDay(10), 25_000, "Kopi")
		var none *uuid.UUID
		got, err := f.svc.Revise(f.ctx, tx.ID, transaction.RevisePatch{PeriodID: &none})
		if err != nil {
			t.Fatalf("Revise: %v", err)
		}
		if got.PeriodID != nil {
			t.Errorf("period=%v want nil; **uuid.UUID must distinguish clear from leave-alone", got.PeriodID)
		}
	})

	t.Run("clears the category when the patch names nil", func(t *testing.T) {
		f := newFixture(t)
		tx := f.expense(t, augustDay(10), 25_000, "Kopi")
		var none *uuid.UUID
		got, err := f.svc.Revise(f.ctx, tx.ID, transaction.RevisePatch{CategoryID: &none})
		if err != nil {
			t.Fatalf("Revise: %v", err)
		}
		if got.CategoryID != nil {
			t.Errorf("category=%v want nil", got.CategoryID)
		}
	})

	t.Run("refuses an amount in another currency", func(t *testing.T) {
		f := newFixture(t)
		tx := f.expense(t, augustDay(10), 25_000, "Kopi")
		usd := money.New(100, money.Currency("USD"))
		_, err := f.svc.Revise(f.ctx, tx.ID, transaction.RevisePatch{Amount: &usd})
		if !transaction.IsCurrencyMismatchError(err) {
			t.Errorf("want IsCurrencyMismatchError, got %v", err)
		}
	})

	t.Run("is scoped to the caller's project", func(t *testing.T) {
		f := newFixture(t)
		tx := f.expense(t, augustDay(10), 25_000, "Kopi")
		other := tenant.Into(context.Background(), tenant.Context{
			OrgID: f.tc.OrgID, ProjectID: uuid.New(), UserID: f.tc.UserID,
		})
		_, err := f.svc.Revise(other, tx.ID, transaction.RevisePatch{Note: ptr("nope")})
		if !transaction.IsNotFoundError(err) {
			t.Errorf("want IsNotFoundError, got %v", err)
		}
	})
}

func TestService_ByID_ScopeCheck(t *testing.T) {
	f := newFixture(t)
	tx := f.expense(t, augustDay(10), 25_000, "Kopi")

	raw, err := f.store.ByID(f.ctx, tx.ID)
	if err != nil || raw == nil {
		t.Fatalf("the fake must not filter by scope, or this test proves nothing: %v", err)
	}

	got, err := f.svc.ByID(f.ctx, tx.ID)
	if err != nil {
		t.Fatalf("ByID in scope: %v", err)
	}
	if got.ID != tx.ID {
		t.Errorf("id=%v", got.ID)
	}

	otherProject := tenant.Into(context.Background(), tenant.Context{
		OrgID: f.tc.OrgID, ProjectID: uuid.New(), UserID: f.tc.UserID,
	})
	if _, err := f.svc.ByID(otherProject, tx.ID); !transaction.IsNotFoundError(err) {
		t.Errorf("sibling project: want IsNotFoundError, got %v", err)
	}

	otherOrg := tenant.Into(context.Background(), tenant.Context{
		OrgID: uuid.New(), ProjectID: f.tc.ProjectID, UserID: f.tc.UserID,
	})
	if _, err := f.svc.ByID(otherOrg, tx.ID); !transaction.IsNotFoundError(err) {
		t.Errorf("other org: want IsNotFoundError, got %v", err)
	}
}

func TestService_Delete(t *testing.T) {
	t.Run("removes the row", func(t *testing.T) {
		f := newFixture(t)
		tx := f.expense(t, augustDay(10), 25_000, "Kopi")
		if err := f.svc.Delete(f.ctx, tx.ID); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if _, err := f.svc.ByID(f.ctx, tx.ID); !transaction.IsNotFoundError(err) {
			t.Errorf("after Delete: %v", err)
		}
	})

	t.Run("refuses a locked period", func(t *testing.T) {
		f := newFixture(t)
		tx := f.expense(t, augustDay(10), 25_000, "Kopi")
		f.periods.infos[f.august] = transaction.PeriodInfo{ID: f.august, Locked: true, PrevID: ptr(f.july), NextID: ptr(f.september)}
		if err := f.svc.Delete(f.ctx, tx.ID); !transaction.IsPeriodLockedError(err) {
			t.Errorf("want IsPeriodLockedError, got %v", err)
		}
	})

	t.Run("is scoped to the caller's project", func(t *testing.T) {
		f := newFixture(t)
		tx := f.expense(t, augustDay(10), 25_000, "Kopi")
		other := tenant.Into(context.Background(), tenant.Context{
			OrgID: f.tc.OrgID, ProjectID: uuid.New(), UserID: f.tc.UserID,
		})
		if err := f.svc.Delete(other, tx.ID); !transaction.IsNotFoundError(err) {
			t.Errorf("want IsNotFoundError, got %v", err)
		}
		if _, err := f.store.ByID(f.ctx, tx.ID); err != nil {
			t.Errorf("the row must survive a cross-project Delete: %v", err)
		}
	})
}

func TestService_List_NextCursor(t *testing.T) {
	f := newFixture(t)
	for day := 1; day <= 3; day++ {
		f.expense(t, augustDay(day), int64(day)*1_000, fmt.Sprintf("row %d", day))
	}

	page1, next, err := f.svc.List(f.ctx, transaction.ListOpts{Limit: 2})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("page1=%d want 2", len(page1))
	}
	if next == nil {
		t.Fatal("a full page must carry a next cursor")
	}

	page2, next2, err := f.svc.List(f.ctx, transaction.ListOpts{Limit: 2, After: next})
	if err != nil {
		t.Fatalf("List page2: %v", err)
	}
	if len(page2) != 1 {
		t.Fatalf("page2=%d want 1", len(page2))
	}
	if next2 != nil {
		t.Error("a short page must carry no next cursor")
	}

	seen := map[uuid.UUID]bool{}
	for _, tx := range append(page1, page2...) {
		if seen[tx.ID] {
			t.Errorf("row %s appeared on two pages", tx.ID)
		}
		seen[tx.ID] = true
	}
	if !page1[0].OccurredAt.After(page1[1].OccurredAt) {
		t.Error("List must order newest first")
	}
}

func TestService_Balances(t *testing.T) {
	f := newFixture(t)
	if err := f.svc.RecordOpening(f.ctx, f.walletIDR, idr(1_000_000), augustDay(1), f.tc.UserID); err != nil {
		t.Fatalf("RecordOpening: %v", err)
	}
	f.expense(t, augustDay(2), 250_000, "Groceries")
	if _, err := f.svc.Record(f.ctx, transaction.RecordInput{
		WalletID:   f.walletIDR,
		ToWalletID: &f.walletIDR2,
		Kind:       transaction.KindTransfer,
		Amount:     idr(100_000),
		OccurredAt: augustDay(3),
	}); err != nil {
		t.Fatalf("transfer: %v", err)
	}

	all, err := f.svc.Balances(f.ctx)
	if err != nil {
		t.Fatalf("Balances: %v", err)
	}
	if all[f.walletIDR].Minor != 650_000 {
		t.Errorf("source balance=%v want 650000", all[f.walletIDR])
	}
	if all[f.walletIDR2].Minor != 100_000 {
		t.Errorf("destination balance=%v want 100000", all[f.walletIDR2])
	}

	one, err := f.svc.Balance(f.ctx, f.walletIDR2)
	if err != nil {
		t.Fatalf("Balance: %v", err)
	}
	if one.Minor != 100_000 {
		t.Errorf("Balance=%v want 100000", one)
	}
}

func TestService_Balance_OfAWalletWithNoTransactions(t *testing.T) {
	f := newFixture(t)
	got, err := f.svc.Balance(f.ctx, f.walletUSD)
	if err != nil {
		t.Fatalf("Balance: %v", err)
	}
	if got.Minor != 0 {
		t.Errorf("Minor=%d want 0", got.Minor)
	}
	if got.Currency != money.Currency("USD") {
		t.Errorf("Currency=%q want USD; a zero balance must still be usable in money arithmetic", got.Currency)
	}
}

func TestService_Adjust(t *testing.T) {
	t.Run("writes the difference as an adjustment_in", func(t *testing.T) {
		f := newFixture(t)
		if err := f.svc.RecordOpening(f.ctx, f.walletIDR, idr(100_000), augustDay(1), f.tc.UserID); err != nil {
			t.Fatal(err)
		}
		got, err := f.svc.Adjust(f.ctx, f.walletIDR, idr(150_000), augustDay(5), f.tc.UserID)
		if err != nil {
			t.Fatalf("Adjust: %v", err)
		}
		if got == nil {
			t.Fatal("Adjust must return the written row")
		}
		if got.Kind != transaction.KindAdjustmentIn {
			t.Errorf("kind=%s want adjustment_in", got.Kind)
		}
		if got.Amount.Minor != 50_000 {
			t.Errorf("amount=%v want 50000", got.Amount)
		}
		bal, _ := f.svc.Balance(f.ctx, f.walletIDR)
		if bal.Minor != 150_000 {
			t.Errorf("balance after Adjust=%v want 150000", bal)
		}
	})

	t.Run("writes an adjustment_out when the target is lower", func(t *testing.T) {
		f := newFixture(t)
		if err := f.svc.RecordOpening(f.ctx, f.walletIDR, idr(100_000), augustDay(1), f.tc.UserID); err != nil {
			t.Fatal(err)
		}
		got, err := f.svc.Adjust(f.ctx, f.walletIDR, idr(40_000), augustDay(5), f.tc.UserID)
		if err != nil {
			t.Fatalf("Adjust: %v", err)
		}
		if got.Kind != transaction.KindAdjustmentOut || got.Amount.Minor != 60_000 {
			t.Errorf("got kind=%s amount=%v want adjustment_out 60000", got.Kind, got.Amount)
		}
	})

	t.Run("is a no-op when the balance already matches", func(t *testing.T) {
		f := newFixture(t)
		if err := f.svc.RecordOpening(f.ctx, f.walletIDR, idr(100_000), augustDay(1), f.tc.UserID); err != nil {
			t.Fatal(err)
		}
		got, err := f.svc.Adjust(f.ctx, f.walletIDR, idr(100_000), augustDay(5), f.tc.UserID)
		if err != nil {
			t.Fatalf("Adjust: %v", err)
		}
		if got != nil {
			t.Errorf("want (nil, nil), got %+v", got)
		}
	})

	t.Run("refuses a target in another currency", func(t *testing.T) {
		f := newFixture(t)
		_, err := f.svc.Adjust(f.ctx, f.walletIDR, money.New(100, money.Currency("USD")), augustDay(5), f.tc.UserID)
		if !transaction.IsCurrencyMismatchError(err) {
			t.Errorf("want IsCurrencyMismatchError, got %v", err)
		}
	})

	t.Run("seeds an empty wallet from zero", func(t *testing.T) {
		f := newFixture(t)
		got, err := f.svc.Adjust(f.ctx, f.walletIDR, idr(75_000), augustDay(5), f.tc.UserID)
		if err != nil {
			t.Fatalf("Adjust: %v", err)
		}
		if got.Kind != transaction.KindAdjustmentIn || got.Amount.Minor != 75_000 {
			t.Errorf("got kind=%s amount=%v", got.Kind, got.Amount)
		}
	})
}

func TestService_SuggestCategory(t *testing.T) {
	f := newFixture(t)
	older := f.expense(t, augustDay(1), 20_000, "Kopi Kenangan")
	_ = older

	newerCat := uuid.New()
	f.categories[newerCat] = transaction.CategoryInfo{ID: newerCat, Kind: "expense"}
	if _, err := f.svc.Record(f.ctx, transaction.RecordInput{
		WalletID:   f.walletIDR,
		Kind:       transaction.KindExpense,
		Amount:     idr(22_000),
		CategoryID: &newerCat,
		Note:       "  kopi    KENANGAN ",
		OccurredAt: augustDay(9),
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	got, ok, err := f.svc.SuggestCategory(f.ctx, "KOPI kenangan")
	if err != nil {
		t.Fatalf("SuggestCategory: %v", err)
	}
	if !ok {
		t.Fatal("want a suggestion")
	}
	if got != newerCat {
		t.Errorf("suggested=%v want the newest matching row's category %v", got, newerCat)
	}

	if _, ok, err := f.svc.SuggestCategory(f.ctx, "never seen"); err != nil || ok {
		t.Errorf("unknown note: ok=%v err=%v", ok, err)
	}
	if _, ok, err := f.svc.SuggestCategory(f.ctx, "   "); err != nil || ok {
		t.Errorf("blank note: ok=%v err=%v", ok, err)
	}
}

func TestService_RecordOpening(t *testing.T) {
	f := newFixture(t)
	by := uuid.New()
	noUser := tenant.Into(context.Background(), tenant.Context{OrgID: f.tc.OrgID, ProjectID: f.tc.ProjectID})
	if err := f.svc.RecordOpening(noUser, f.walletIDR, idr(500_000), augustDay(1), by); err != nil {
		t.Fatalf("RecordOpening: %v", err)
	}
	rows, _, err := f.svc.List(f.ctx, transaction.ListOpts{Kinds: []transaction.Kind{transaction.KindOpening}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows=%d want 1", len(rows))
	}
	if rows[0].CreatedBy != by {
		t.Errorf("CreatedBy=%v want the explicit actor %v", rows[0].CreatedBy, by)
	}
	if rows[0].Kind != transaction.KindOpening {
		t.Errorf("kind=%s", rows[0].Kind)
	}
}

func TestService_TenantMissing(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.Record(context.Background(), transaction.RecordInput{
		WalletID:   f.walletIDR,
		Kind:       transaction.KindExpense,
		Amount:     idr(1_000),
		OccurredAt: augustDay(1),
	})
	if !tenant.IsMissingError(err) {
		t.Errorf("want tenant.MissingError, got %v", err)
	}
}

func TestService_Adjust_RunsInsideAUnitOfWorkAndLocksTheWallet(t *testing.T) {
	f := newFixture(t)
	if err := f.svc.RecordOpening(f.ctx, f.walletIDR, idr(100_000), augustDay(1), f.tc.UserID); err != nil {
		t.Fatal(err)
	}
	before := *f.uowEntries

	if _, err := f.svc.Adjust(f.ctx, f.walletIDR, idr(150_000), augustDay(5), f.tc.UserID); err != nil {
		t.Fatalf("Adjust: %v", err)
	}
	if got := *f.uowEntries - before; got != 1 {
		t.Errorf("Adjust entered the unit of work %d times, want exactly 1: the read-modify-write must be one transaction", got)
	}

	locks := f.store.Locks()
	if len(locks) != 1 || locks[0] != f.walletIDR {
		t.Errorf("Adjust locked %v, want exactly [%v]: without the lock two concurrent adjustments both read the stale balance", locks, f.walletIDR)
	}
}

func TestService_Adjust_LocksBeforeReadingEvenWhenTheBalanceAlreadyMatches(t *testing.T) {
	f := newFixture(t)
	if err := f.svc.RecordOpening(f.ctx, f.walletIDR, idr(100_000), augustDay(1), f.tc.UserID); err != nil {
		t.Fatal(err)
	}
	got, err := f.svc.Adjust(f.ctx, f.walletIDR, idr(100_000), augustDay(5), f.tc.UserID)
	if err != nil || got != nil {
		t.Fatalf("Adjust: got %+v err %v, want (nil, nil)", got, err)
	}
	if locks := f.store.Locks(); len(locks) != 1 {
		t.Errorf("locks=%v; the decision to write nothing is only sound if the balance was read under the lock", locks)
	}
}

func TestService_Adjust_PropagatesAUnitOfWorkFailure(t *testing.T) {
	f := newFixture(t)
	boom := errors.New("uow refused")
	f.svc = transaction.NewService(f.store, slog.New(slog.NewTextHandler(io.Discard, nil)),
		func(_ context.Context, _ string, err error, _ ...any) *apperror.AppError {
			return apperror.New("yasaku.unexpected", err.Error(), codes.Internal,
				&apperrorv1.ErrorDetail{Code: "yasaku.unexpected"}).WithCause(err)
		},
		transaction.WalletReaderFunc(func(_ context.Context, _, _, id uuid.UUID) (transaction.WalletInfo, error) {
			return f.wallets[id], nil
		}),
		transaction.CategoryReaderFunc(func(_ context.Context, _, _, id uuid.UUID) (transaction.CategoryInfo, error) {
			return f.categories[id], nil
		}),
		f.periods,
		func(context.Context, func(context.Context) error) error { return boom },
	)
	_, err := f.svc.Adjust(f.ctx, f.walletIDR, idr(150_000), augustDay(5), f.tc.UserID)
	if !errors.Is(err, boom) {
		t.Errorf("want the unit of work error, got %v", err)
	}
}

func TestService_Balances_OmitsWalletsWithNoTransactions(t *testing.T) {
	f := newFixture(t)
	f.expense(t, augustDay(2), 25_000, "Kopi")

	all, err := f.svc.Balances(f.ctx)
	if err != nil {
		t.Fatalf("Balances: %v", err)
	}
	if _, ok := all[f.walletIDR]; !ok {
		t.Error("a wallet with transactions must be present")
	}
	if _, ok := all[f.walletUSD]; ok {
		t.Error("Balances is documented as covering only wallets with at least one transaction")
	}
	for id, amount := range all {
		if amount.Currency == "" {
			t.Errorf("wallet %s has a currency-less amount; every value in the map must be usable in money arithmetic", id)
		}
	}
}
