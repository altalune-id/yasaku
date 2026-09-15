package transaction_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"altalune.id/yasaku/internal/transaction"
	"altalune.id/yasaku/money"
)

func idr(minor int64) money.Amount { return money.New(minor, money.IDR) }

func baseParams(walletID uuid.UUID) transaction.NewParams {
	return transaction.NewParams{
		WalletID:   walletID,
		Kind:       transaction.KindExpense,
		Amount:     idr(25_000),
		Note:       "Kopi Kenangan",
		OccurredAt: time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC),
		CreatedBy:  uuid.New(),
	}
}

func TestParseKind(t *testing.T) {
	for _, s := range []string{"income", "expense", "transfer", "opening", "adjustment_in", "adjustment_out"} {
		k, err := transaction.ParseKind(s)
		if err != nil {
			t.Fatalf("ParseKind(%q): %v", s, err)
		}
		if string(k) != s {
			t.Errorf("ParseKind(%q)=%q", s, k)
		}
	}
	if _, err := transaction.ParseKind("refund"); !transaction.IsInvalidKindError(err) {
		t.Errorf("ParseKind(refund): want IsInvalidKindError, got %v", err)
	}
	if _, err := transaction.ParseKind(""); !transaction.IsInvalidKindError(err) {
		t.Errorf("ParseKind(empty): want IsInvalidKindError, got %v", err)
	}
}

func TestKindPredicates(t *testing.T) {
	inflow := map[transaction.Kind]bool{
		transaction.KindIncome:        true,
		transaction.KindOpening:       true,
		transaction.KindAdjustmentIn:  true,
		transaction.KindExpense:       false,
		transaction.KindTransfer:      false,
		transaction.KindAdjustmentOut: false,
	}
	for k, want := range inflow {
		if got := k.IsInflow(); got != want {
			t.Errorf("%s.IsInflow()=%v want %v", k, got, want)
		}
	}
	allows := map[transaction.Kind]bool{
		transaction.KindIncome:        true,
		transaction.KindExpense:       true,
		transaction.KindTransfer:      false,
		transaction.KindOpening:       false,
		transaction.KindAdjustmentIn:  false,
		transaction.KindAdjustmentOut: false,
	}
	for k, want := range allows {
		if got := k.AllowsCategory(); got != want {
			t.Errorf("%s.AllowsCategory()=%v want %v", k, got, want)
		}
	}
}

func TestNew_HappyPath(t *testing.T) {
	orgID, projID, walletID := uuid.New(), uuid.New(), uuid.New()
	p := baseParams(walletID)

	got, err := transaction.New(orgID, projID, p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got.ID == uuid.Nil {
		t.Error("New must assign an id")
	}
	if got.OrgID != orgID || got.ProjectID != projID {
		t.Error("New must carry the tenant scope")
	}
	if got.Amount.Minor != 25_000 || got.Amount.Currency != money.IDR {
		t.Errorf("amount=%v", got.Amount)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Error("New must stamp CreatedAt/UpdatedAt")
	}
}

func TestNew_Invariants(t *testing.T) {
	orgID, projID, walletID, otherWallet := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	catID := uuid.New()

	t.Run("zero amount", func(t *testing.T) {
		p := baseParams(walletID)
		p.Amount = idr(0)
		_, err := transaction.New(orgID, projID, p)
		if !transaction.IsInvalidAmountError(err) {
			t.Errorf("want IsInvalidAmountError, got %v", err)
		}
	})
	t.Run("negative amount", func(t *testing.T) {
		p := baseParams(walletID)
		p.Amount = idr(-1)
		_, err := transaction.New(orgID, projID, p)
		if !transaction.IsInvalidAmountError(err) {
			t.Errorf("want IsInvalidAmountError, got %v", err)
		}
	})
	t.Run("unknown currency", func(t *testing.T) {
		p := baseParams(walletID)
		p.Amount = money.New(100, money.Currency("XXX"))
		_, err := transaction.New(orgID, projID, p)
		if !transaction.IsInvalidAmountError(err) {
			t.Errorf("want IsInvalidAmountError, got %v", err)
		}
	})
	t.Run("transfer without a destination wallet", func(t *testing.T) {
		p := baseParams(walletID)
		p.Kind = transaction.KindTransfer
		_, err := transaction.New(orgID, projID, p)
		if !transaction.IsInvalidKindError(err) {
			t.Errorf("want IsInvalidKindError, got %v", err)
		}
	})
	t.Run("transfer to the same wallet", func(t *testing.T) {
		p := baseParams(walletID)
		p.Kind = transaction.KindTransfer
		p.ToWalletID = &walletID
		_, err := transaction.New(orgID, projID, p)
		if !transaction.IsSameWalletError(err) {
			t.Errorf("want IsSameWalletError, got %v", err)
		}
	})
	t.Run("category on a transfer", func(t *testing.T) {
		p := baseParams(walletID)
		p.Kind = transaction.KindTransfer
		p.ToWalletID = &otherWallet
		p.CategoryID = &catID
		_, err := transaction.New(orgID, projID, p)
		if !transaction.IsInvalidKindError(err) {
			t.Errorf("want IsInvalidKindError, got %v", err)
		}
	})
	t.Run("destination wallet on a non-transfer", func(t *testing.T) {
		p := baseParams(walletID)
		p.ToWalletID = &otherWallet
		_, err := transaction.New(orgID, projID, p)
		if !transaction.IsInvalidKindError(err) {
			t.Errorf("want IsInvalidKindError, got %v", err)
		}
	})
	t.Run("category on an opening", func(t *testing.T) {
		p := baseParams(walletID)
		p.Kind = transaction.KindOpening
		p.CategoryID = &catID
		_, err := transaction.New(orgID, projID, p)
		if !transaction.IsInvalidKindError(err) {
			t.Errorf("want IsInvalidKindError, got %v", err)
		}
	})
	t.Run("note over 500 runes", func(t *testing.T) {
		p := baseParams(walletID)
		long := ""
		for range 501 {
			long += "é"
		}
		p.Note = long
		_, err := transaction.New(orgID, projID, p)
		if !transaction.IsInvalidNoteError(err) {
			t.Errorf("want IsInvalidNoteError, got %v", err)
		}
	})
	t.Run("note at exactly 500 runes is accepted", func(t *testing.T) {
		p := baseParams(walletID)
		long := ""
		for range 500 {
			long += "é"
		}
		p.Note = long
		if _, err := transaction.New(orgID, projID, p); err != nil {
			t.Errorf("500 runes must be accepted, got %v", err)
		}
	})
	t.Run("unknown kind", func(t *testing.T) {
		p := baseParams(walletID)
		p.Kind = transaction.Kind("refund")
		_, err := transaction.New(orgID, projID, p)
		if !transaction.IsInvalidKindError(err) {
			t.Errorf("want IsInvalidKindError, got %v", err)
		}
	})
}

func TestEffect(t *testing.T) {
	orgID, projID := uuid.New(), uuid.New()
	from, to, other := uuid.New(), uuid.New(), uuid.New()

	t.Run("expense is negative on its wallet", func(t *testing.T) {
		tx, err := transaction.New(orgID, projID, baseParams(from))
		if err != nil {
			t.Fatal(err)
		}
		if got := tx.Effect(from); got.Minor != -25_000 {
			t.Errorf("effect=%v want -25000", got)
		}
		if got := tx.Effect(other); got.Minor != 0 {
			t.Errorf("unrelated wallet effect=%v want 0", got)
		}
	})

	t.Run("income is positive on its wallet", func(t *testing.T) {
		p := baseParams(from)
		p.Kind = transaction.KindIncome
		tx, err := transaction.New(orgID, projID, p)
		if err != nil {
			t.Fatal(err)
		}
		if got := tx.Effect(from); got.Minor != 25_000 {
			t.Errorf("effect=%v want 25000", got)
		}
	})

	t.Run("transfer moves both sides", func(t *testing.T) {
		p := baseParams(from)
		p.Kind = transaction.KindTransfer
		p.ToWalletID = &to
		tx, err := transaction.New(orgID, projID, p)
		if err != nil {
			t.Fatal(err)
		}
		if got := tx.Effect(from); got.Minor != -25_000 {
			t.Errorf("from effect=%v want -25000", got)
		}
		if got := tx.Effect(to); got.Minor != 25_000 {
			t.Errorf("to effect=%v want 25000", got)
		}
		if got := tx.Effect(other); got.Minor != 0 {
			t.Errorf("unrelated effect=%v want 0", got)
		}
	})

	t.Run("adjustments follow their direction", func(t *testing.T) {
		for kind, want := range map[transaction.Kind]int64{
			transaction.KindAdjustmentIn:  25_000,
			transaction.KindAdjustmentOut: -25_000,
			transaction.KindOpening:       25_000,
		} {
			p := baseParams(from)
			p.Kind = kind
			tx, err := transaction.New(orgID, projID, p)
			if err != nil {
				t.Fatalf("%s: %v", kind, err)
			}
			if got := tx.Effect(from); got.Minor != want {
				t.Errorf("%s effect=%v want %d", kind, got, want)
			}
		}
	})

	t.Run("effect keeps the transaction currency", func(t *testing.T) {
		tx, err := transaction.New(orgID, projID, baseParams(from))
		if err != nil {
			t.Fatal(err)
		}
		if tx.Effect(other).Currency != money.IDR {
			t.Errorf("zero effect must keep the currency, got %q", tx.Effect(other).Currency)
		}
	})
}

func TestSetters(t *testing.T) {
	orgID, projID, walletID := uuid.New(), uuid.New(), uuid.New()
	catID := uuid.New()

	t.Run("SetAmount rejects non-positive", func(t *testing.T) {
		tx, _ := transaction.New(orgID, projID, baseParams(walletID))
		if err := tx.SetAmount(idr(0)); !transaction.IsInvalidAmountError(err) {
			t.Errorf("want IsInvalidAmountError, got %v", err)
		}
		if tx.Amount.Minor != 25_000 {
			t.Error("a rejected SetAmount must leave the aggregate untouched")
		}
		if err := tx.SetAmount(idr(30_000)); err != nil {
			t.Fatal(err)
		}
		if tx.Amount.Minor != 30_000 {
			t.Errorf("amount=%v", tx.Amount)
		}
	})

	t.Run("SetNote rejects an over-long note", func(t *testing.T) {
		tx, _ := transaction.New(orgID, projID, baseParams(walletID))
		long := ""
		for range 501 {
			long += "a"
		}
		if err := tx.SetNote(long); !transaction.IsInvalidNoteError(err) {
			t.Errorf("want IsInvalidNoteError, got %v", err)
		}
		if err := tx.SetNote("  Indomaret  "); err != nil {
			t.Fatal(err)
		}
		if tx.Note != "Indomaret" {
			t.Errorf("note=%q want trimmed", tx.Note)
		}
	})

	t.Run("SetCategory refuses a kind that allows none", func(t *testing.T) {
		p := baseParams(walletID)
		p.Kind = transaction.KindOpening
		tx, _ := transaction.New(orgID, projID, p)
		if err := tx.SetCategory(&catID); !transaction.IsInvalidKindError(err) {
			t.Errorf("want IsInvalidKindError, got %v", err)
		}
		if err := tx.SetCategory(nil); err != nil {
			t.Errorf("clearing a category must always be allowed, got %v", err)
		}
	})

	t.Run("SetPeriod, SetWallet and SetOccurredAt", func(t *testing.T) {
		tx, _ := transaction.New(orgID, projID, baseParams(walletID))
		other, period := uuid.New(), uuid.New()
		tx.SetWallet(other)
		tx.SetPeriod(&period)
		at := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
		tx.SetOccurredAt(at)
		if tx.WalletID != other {
			t.Error("SetWallet")
		}
		if tx.PeriodID == nil || *tx.PeriodID != period {
			t.Error("SetPeriod")
		}
		if !tx.OccurredAt.Equal(at) {
			t.Error("SetOccurredAt")
		}
		tx.SetPeriod(nil)
		if tx.PeriodID != nil {
			t.Error("SetPeriod(nil) must clear")
		}
	})
}

func TestNormalizeNote(t *testing.T) {
	cases := map[string]string{
		"  Kopi   Kenangan ": "kopi kenangan",
		"KOPI KENANGAN":      "kopi kenangan",
		"kopi\tkenangan":     "kopi kenangan",
		"kopi\n\nkenangan":   "kopi kenangan",
		"":                   "",
		"   ":                "",
		"Indomaret":          "indomaret",
	}
	for in, want := range cases {
		if got := transaction.NormalizeNote(in); got != want {
			t.Errorf("NormalizeNote(%q)=%q want %q", in, got, want)
		}
	}
}
