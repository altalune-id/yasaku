package wallet

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/money"
)

// OpeningRecorder posts the opening-balance transaction for a freshly created wallet.
type OpeningRecorder interface {
	RecordOpening(ctx context.Context, walletID uuid.UUID, amount money.Amount, at time.Time, by uuid.UUID) error
}

// UnitOfWork runs fn so that every write inside it commits or rolls back together.
type UnitOfWork func(ctx context.Context, fn func(ctx context.Context) error) error

var errNoUnitOfWorkRun = errors.New("wallet: unit of work returned without running the write")

// OpenWorkflow creates a wallet and its opening balance as one atomic step.
type OpenWorkflow struct {
	wallets    *Service
	opening    OpeningRecorder
	uow        UnitOfWork
	log        *slog.Logger
	unexpected apperror.UnexpectedFunc
}

// NewOpenWorkflow constructs an OpenWorkflow with typed dependencies.
func NewOpenWorkflow(
	wallets *Service,
	opening OpeningRecorder,
	uow UnitOfWork,
	log *slog.Logger,
	unexpected apperror.UnexpectedFunc,
) *OpenWorkflow {
	return &OpenWorkflow{
		wallets:    wallets,
		opening:    opening,
		uow:        uow,
		log:        log.With("module", "wallet"),
		unexpected: unexpected,
	}
}

// Run creates the wallet and, when opening is a non-zero amount, records it as the wallet's opening balance.
func (w *OpenWorkflow) Run(ctx context.Context, p Params, opening *money.Amount, at time.Time) (*Wallet, error) {
	ctx, span := tracer.Start(ctx, "wallet.Open")
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	span.SetAttributes(
		attribute.String("org_id", tc.OrgID.String()),
		attribute.String("project_id", tc.ProjectID.String()),
	)

	var created *Wallet
	var inner error
	uowErr := w.uow(ctx, func(ctx context.Context) error {
		created, inner = w.open(ctx, p, opening, at, tc.UserID)
		return inner
	})
	if inner != nil {
		span.RecordError(inner)
		return nil, inner
	}
	if uowErr != nil || created == nil {
		span.RecordError(uowErr)
		return nil, w.unexpected(ctx, "wallet.Open: unit of work", cmp.Or(uowErr, errNoUnitOfWorkRun),
			"org_id", tc.OrgID, "project_id", tc.ProjectID)
	}
	span.SetAttributes(attribute.String("wallet.id", created.ID.String()))
	return created, nil
}

func (w *OpenWorkflow) open(ctx context.Context, p Params, opening *money.Amount, at time.Time, by uuid.UUID) (*Wallet, error) {
	created, err := w.wallets.Create(ctx, p)
	if err != nil {
		return nil, err
	}
	if opening == nil || opening.IsZero() {
		return created, nil
	}
	if err := w.opening.RecordOpening(ctx, created.ID, *opening, at, by); err != nil {
		return nil, err
	}
	return created, nil
}
