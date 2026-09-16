package ledger_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	apperrorv1 "altalune.id/yasaku/gen/go/apperror/v1"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/ledger"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/money"
)

func newSvc(t *testing.T, store ledger.Store) (*ledger.Service, *int) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	calls := 0
	unexpected := func(_ context.Context, _ string, err error, _ ...any) *apperror.AppError {
		calls++
		return apperror.New("yasaku.unexpected", err.Error(), codes.Internal,
			&apperrorv1.ErrorDetail{Code: "yasaku.unexpected"}).WithCause(err)
	}
	return ledger.NewService(store, log, unexpected), &calls
}

func tenantCtx(t *testing.T) (context.Context, tenant.Context) {
	t.Helper()
	tc := tenant.Context{OrgID: uuid.New(), ProjectID: uuid.New(), UserID: uuid.New()}
	return tenant.Into(context.Background(), tc), tc
}

type failingStore struct {
	onSave      error
	onByProject error
}

func (f *failingStore) Save(context.Context, *ledger.Settings) error { return f.onSave }

func (f *failingStore) ByProject(_ context.Context, orgID, projectID uuid.UUID) (*ledger.Settings, error) {
	if f.onByProject != nil {
		return nil, f.onByProject
	}
	return ledger.Defaults(orgID, projectID), nil
}

func TestService_Get_EmptyStoreReturnsDefaultsAndPersistsNothing(t *testing.T) {
	store := fakes.NewLedger()
	svc, unexCalls := newSvc(t, store)
	ctx, tc := tenantCtx(t)

	got, err := svc.Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.OrgID != tc.OrgID || got.ProjectID != tc.ProjectID {
		t.Errorf("scope not from tenant context: %+v", *got)
	}
	if got.Timezone != ledger.DefaultTimezone || got.Currency != money.IDR || got.PeriodStartDay != ledger.DefaultPeriodStartDay {
		t.Errorf("not the defaults: %+v", *got)
	}
	if store.Len() != 0 {
		t.Errorf("Get persisted %d rows, want 0", store.Len())
	}
	if *unexCalls != 0 {
		t.Errorf("unexpected() called %d times", *unexCalls)
	}
}

func TestService_Get_IsDeterministicForAnUnconfiguredProject(t *testing.T) {
	svc, _ := newSvc(t, fakes.NewLedger())
	ctx, _ := tenantCtx(t)

	first, err := svc.Get(ctx)
	if err != nil {
		t.Fatalf("first Get: %v", err)
	}
	second, err := svc.Get(ctx)
	if err != nil {
		t.Fatalf("second Get: %v", err)
	}
	if *first != *second {
		t.Errorf("Get on an unconfigured project must not change between reads:\n first=%+v\nsecond=%+v", *first, *second)
	}
	if !first.UpdatedAt.IsZero() {
		t.Errorf("UpdatedAt=%v, want zero until something is written", first.UpdatedAt)
	}
}

func TestService_Update_PersistsAndIsReadBack(t *testing.T) {
	store := fakes.NewLedger()
	svc, unexCalls := newSvc(t, store)
	ctx, tc := tenantCtx(t)

	usd := money.Currency("USD")
	tz := "Asia/Tokyo"
	day := 25
	updated, err := svc.Update(ctx, ledger.Patch{Timezone: &tz, Currency: &usd, PeriodStartDay: &day})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Timezone != tz || updated.Currency != usd || updated.PeriodStartDay != day {
		t.Errorf("Update returned %+v", *updated)
	}
	if updated.OrgID != tc.OrgID || updated.ProjectID != tc.ProjectID {
		t.Errorf("scope not from tenant context: %+v", *updated)
	}
	if updated.UpdatedAt.IsZero() {
		t.Error("Update must stamp UpdatedAt even when the project had no stored row")
	}
	if store.Len() != 1 {
		t.Fatalf("store holds %d rows, want 1", store.Len())
	}

	got, err := svc.Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Timezone != tz || got.Currency != usd || got.PeriodStartDay != day {
		t.Errorf("second Get returned %+v", *got)
	}
	if *unexCalls != 0 {
		t.Errorf("unexpected() called %d times", *unexCalls)
	}
}

func TestService_Update_PartialPatchKeepsStoredValues(t *testing.T) {
	store := fakes.NewLedger()
	svc, _ := newSvc(t, store)
	ctx, _ := tenantCtx(t)

	tz := "Asia/Tokyo"
	if _, err := svc.Update(ctx, ledger.Patch{Timezone: &tz}); err != nil {
		t.Fatalf("first Update: %v", err)
	}
	day := 15
	got, err := svc.Update(ctx, ledger.Patch{PeriodStartDay: &day})
	if err != nil {
		t.Fatalf("second Update: %v", err)
	}
	if got.Timezone != tz {
		t.Errorf("second Update dropped the stored timezone: %q", got.Timezone)
	}
	if got.PeriodStartDay != day {
		t.Errorf("PeriodStartDay=%d want %d", got.PeriodStartDay, day)
	}
}

func TestService_Update_InvalidPatchPersistsNothing(t *testing.T) {
	tests := []struct {
		name  string
		patch ledger.Patch
		check func(error) bool
	}{
		{"bad timezone", ledger.Patch{Timezone: ptr("Mars/Olympus")}, ledger.IsInvalidTimezoneError},
		{"bad start day", ledger.Patch{PeriodStartDay: ptr(0)}, ledger.IsInvalidStartDayError},
		{"bad currency", ledger.Patch{Currency: ptr(money.Currency("XXX"))}, ledger.IsUnknownCurrencyError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := fakes.NewLedger()
			svc, unexCalls := newSvc(t, store)
			ctx, _ := tenantCtx(t)

			_, err := svc.Update(ctx, tt.patch)
			if !tt.check(err) {
				t.Fatalf("Update: wrong error: %T %v", err, err)
			}
			if store.Len() != 0 {
				t.Errorf("a rejected Update persisted %d rows, want 0", store.Len())
			}
			if *unexCalls != 0 {
				t.Errorf("an invariant error must not route through unexpected")
			}
		})
	}
}

func TestService_Ports_FallBackToDefaults(t *testing.T) {
	svc, _ := newSvc(t, fakes.NewLedger())
	ctx, tc := tenantCtx(t)

	loc, err := svc.Location(ctx, tc.OrgID, tc.ProjectID)
	if err != nil {
		t.Fatalf("Location: %v", err)
	}
	if loc.String() != ledger.DefaultTimezone {
		t.Errorf("Location=%q want %q", loc, ledger.DefaultTimezone)
	}
	day, err := svc.StartDay(ctx, tc.OrgID, tc.ProjectID)
	if err != nil {
		t.Fatalf("StartDay: %v", err)
	}
	if day != ledger.DefaultPeriodStartDay {
		t.Errorf("StartDay=%d want %d", day, ledger.DefaultPeriodStartDay)
	}
	cur, err := svc.DefaultCurrency(ctx, tc.OrgID, tc.ProjectID)
	if err != nil {
		t.Fatalf("DefaultCurrency: %v", err)
	}
	if cur != money.IDR {
		t.Errorf("DefaultCurrency=%q want IDR", cur)
	}
}

func TestService_Ports_ReadStoredSettings(t *testing.T) {
	store := fakes.NewLedger()
	svc, _ := newSvc(t, store)
	ctx, tc := tenantCtx(t)

	seeded := ledger.Defaults(tc.OrgID, tc.ProjectID)
	if err := seeded.Apply(ledger.Patch{
		Timezone:       ptr("Asia/Tokyo"),
		Currency:       ptr(money.Currency("JPY")),
		PeriodStartDay: ptr(20),
	}); err != nil {
		t.Fatal(err)
	}
	store.Seed(seeded)

	loc, err := svc.Location(ctx, tc.OrgID, tc.ProjectID)
	if err != nil {
		t.Fatalf("Location: %v", err)
	}
	if loc.String() != "Asia/Tokyo" {
		t.Errorf("Location=%q", loc)
	}
	day, err := svc.StartDay(ctx, tc.OrgID, tc.ProjectID)
	if err != nil {
		t.Fatalf("StartDay: %v", err)
	}
	if day != 20 {
		t.Errorf("StartDay=%d want 20", day)
	}
	cur, err := svc.DefaultCurrency(ctx, tc.OrgID, tc.ProjectID)
	if err != nil {
		t.Fatalf("DefaultCurrency: %v", err)
	}
	if cur != money.Currency("JPY") {
		t.Errorf("DefaultCurrency=%q want JPY", cur)
	}
}

func TestService_MissingTenant(t *testing.T) {
	svc, _ := newSvc(t, fakes.NewLedger())

	if _, err := svc.Get(context.Background()); !tenant.IsMissingError(err) {
		t.Errorf("Get: want tenant.MissingError, got %T %v", err, err)
	}
	if _, err := svc.Update(context.Background(), ledger.Patch{}); !tenant.IsMissingError(err) {
		t.Errorf("Update: want tenant.MissingError, got %T %v", err, err)
	}
}

func TestService_StoreFailuresRouteThroughUnexpected(t *testing.T) {
	t.Run("read", func(t *testing.T) {
		svc, unexCalls := newSvc(t, &failingStore{onByProject: errors.New("boom")})
		ctx, _ := tenantCtx(t)
		if _, err := svc.Get(ctx); err == nil {
			t.Fatal("want error")
		}
		if *unexCalls != 1 {
			t.Errorf("unexpected() called %d times, want 1", *unexCalls)
		}
	})
	t.Run("write", func(t *testing.T) {
		svc, unexCalls := newSvc(t, &failingStore{onSave: errors.New("boom")})
		ctx, _ := tenantCtx(t)
		if _, err := svc.Update(ctx, ledger.Patch{}); err == nil {
			t.Fatal("want error")
		}
		if *unexCalls != 1 {
			t.Errorf("unexpected() called %d times, want 1", *unexCalls)
		}
	})
}
