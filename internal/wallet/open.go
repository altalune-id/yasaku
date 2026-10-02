package wallet

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/money"
)

// OpeningRecorder posts the opening-balance transaction for a freshly created wallet.
type OpeningRecorder interface {
	RecordOpening(ctx context.Context, walletID uuid.UUID, amount money.Amount, at time.Time, by uuid.UUID) error
}

// OpeningDate is when an opening balance is dated; Moved reports that it was moved out of a closed period, and Date is At's day in the project timezone.
type OpeningDate struct {
	At    time.Time
	Date  civil.Date
	Moved bool
}

// OpeningDater dates an opening balance recorded at at, never inside a closed period.
type OpeningDater interface {
	OpeningDate(ctx context.Context, at time.Time) (OpeningDate, error)
}

// OpeningDaterFunc adapts a function to OpeningDater.
type OpeningDaterFunc func(ctx context.Context, at time.Time) (OpeningDate, error)

// OpeningDate calls f.
func (f OpeningDaterFunc) OpeningDate(ctx context.Context, at time.Time) (OpeningDate, error) {
	return f(ctx, at)
}

// UnitOfWork runs fn so that every write inside it commits or rolls back together.
type UnitOfWork func(ctx context.Context, fn func(ctx context.Context) error) error

var errNoUnitOfWorkRun = errors.New("wallet: unit of work returned without running the write")

// OpenWorkflow creates a wallet and its opening balance as one atomic step.
type OpenWorkflow struct {
	wallets    *Service
	opening    OpeningRecorder
	dater      OpeningDater
	uow        UnitOfWork
	log        *slog.Logger
	unexpected apperror.UnexpectedFunc
}

// NewOpenWorkflow constructs an OpenWorkflow with typed dependencies.
func NewOpenWorkflow(
	wallets *Service,
	opening OpeningRecorder,
	dater OpeningDater,
	uow UnitOfWork,
	log *slog.Logger,
	unexpected apperror.UnexpectedFunc,
) *OpenWorkflow {
	return &OpenWorkflow{
		wallets:    wallets,
		opening:    opening,
		dater:      dater,
		uow:        uow,
		log:        log.With("module", "wallet"),
		unexpected: unexpected,
	}
}

// Run creates the wallet and, when opening is a non-zero amount, records it as the wallet's opening balance, dated by the OpeningDater.
func (w *OpenWorkflow) Run(ctx context.Context, p Params, opening *money.Amount, at time.Time) (*Wallet, OpeningDate, error) {
	ctx, span := tracer.Start(ctx, "wallet.Open")
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, OpeningDate{}, err
	}
	span.SetAttributes(
		attribute.String("org_id", tc.OrgID.String()),
		attribute.String("project_id", tc.ProjectID.String()),
	)

	var created *Wallet
	var dated OpeningDate
	var inner error
	uowErr := w.uow(ctx, func(ctx context.Context) error {
		created, dated, inner = w.open(ctx, p, opening, at, tc.UserID)
		return inner
	})
	if inner != nil {
		span.RecordError(inner)
		return nil, OpeningDate{}, inner
	}
	if uowErr != nil || created == nil {
		span.RecordError(uowErr)
		return nil, OpeningDate{}, w.unexpected(ctx, "wallet.Open: unit of work", cmp.Or(uowErr, errNoUnitOfWorkRun),
			"org_id", tc.OrgID, "project_id", tc.ProjectID)
	}
	span.SetAttributes(attribute.String("wallet.id", created.ID.String()), attribute.Bool("wallet.opening_moved", dated.Moved))
	return created, dated, nil
}

// Preview dates an opening balance exactly as Run would, writing nothing.
func (w *OpenWorkflow) Preview(ctx context.Context, opening *money.Amount, at time.Time) (OpeningDate, error) {
	ctx, span := tracer.Start(ctx, "wallet.OpenPreview")
	defer span.End()

	if _, err := tenant.From(ctx); err != nil {
		return OpeningDate{}, err
	}
	return w.date(ctx, opening, at)
}

func (w *OpenWorkflow) open(ctx context.Context, p Params, opening *money.Amount, at time.Time, by uuid.UUID) (*Wallet, OpeningDate, error) {
	created, err := w.wallets.Create(ctx, p)
	if err != nil {
		return nil, OpeningDate{}, err
	}
	dated, err := w.date(ctx, opening, at)
	if err != nil {
		return nil, OpeningDate{}, err
	}
	if opening == nil || opening.IsZero() {
		return created, dated, nil
	}
	if err := w.opening.RecordOpening(ctx, created.ID, *opening, dated.At, by); err != nil {
		return nil, OpeningDate{}, err
	}
	return created, dated, nil
}

func (w *OpenWorkflow) date(ctx context.Context, opening *money.Amount, at time.Time) (OpeningDate, error) {
	if opening == nil || opening.IsZero() {
		return OpeningDate{At: at}, nil
	}
	return w.dater.OpeningDate(ctx, at)
}
