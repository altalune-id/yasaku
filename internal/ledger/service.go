package ledger

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/platform/audit"
	"altalune.id/yasaku/internal/platform/authz"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/money"
)

//nolint:gochecknoglobals // OTel tracer is a package-level fixture, not runtime state.
var tracer = otel.Tracer("altalune.id/yasaku/internal/ledger")

// Service is the ledger settings driving port.
type Service struct {
	store      Store
	log        *slog.Logger
	unexpected apperror.UnexpectedFunc
	uow        tenant.UnitOfWork
	audit      Auditor
}

// Auditor records a settings change in the caller's unit of work.
type Auditor interface {
	Record(ctx context.Context, e audit.Event) error
}

// Option configures a Service.
type Option func(*Service)

// WithAudit saves each settings change in uow and records ledger.settings_changed.
func WithAudit(uow tenant.UnitOfWork, a Auditor) Option {
	return func(s *Service) { s.uow, s.audit = uow, a }
}

// NewService binds the service to its dependencies.
func NewService(store Store, log *slog.Logger, unexpected apperror.UnexpectedFunc, opts ...Option) *Service {
	s := &Service{store: store, log: log.With("module", "ledger"), unexpected: unexpected,
		uow: func(ctx context.Context, fn func(ctx context.Context) error) error { return fn(ctx) }}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Get returns the caller's project settings, or the defaults when none were ever saved.
func (s *Service) Get(ctx context.Context) (*Settings, error) {
	ctx, span := tracer.Start(ctx, "ledger.Get")
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	span.SetAttributes(
		attribute.String("org_id", tc.OrgID.String()),
		attribute.String("project_id", tc.ProjectID.String()),
	)
	return s.effective(ctx, "ledger.Get", tc.OrgID, tc.ProjectID)
}

// Update validates p against the current settings and persists the result.
func (s *Service) Update(ctx context.Context, p Patch) (*Settings, error) {
	ctx, span := tracer.Start(ctx, "ledger.Update")
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	span.SetAttributes(
		attribute.String("org_id", tc.OrgID.String()),
		attribute.String("project_id", tc.ProjectID.String()),
	)
	if err := authz.Can(ctx, authz.LedgerSettings); err != nil {
		span.RecordError(err)
		return nil, err
	}

	cur, err := s.effective(ctx, "ledger.Update", tc.OrgID, tc.ProjectID)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	if applyErr := cur.Apply(p); applyErr != nil {
		span.RecordError(applyErr)
		return nil, applyErr
	}
	saveErr := s.uow(ctx, func(ctx context.Context) error {
		if err := s.store.Save(ctx, cur); err != nil {
			return err
		}
		if s.audit == nil {
			return nil
		}
		return s.audit.Record(ctx, audit.Event{Action: "ledger.settings_changed", TargetKind: "project", TargetID: tc.ProjectID.String(),
			ProjectID: tc.ProjectID, Detail: map[string]string{
				"timezone": cur.Timezone, "currency": string(cur.Currency), "start_day": strconv.Itoa(cur.PeriodStartDay),
			}})
	})
	if saveErr != nil {
		span.RecordError(saveErr)
		if IsNotFoundError(saveErr) {
			return nil, saveErr
		}
		return nil, s.unexpected(ctx, "ledger.Update: save", saveErr,
			"org_id", tc.OrgID, "project_id", tc.ProjectID)
	}
	return cur, nil
}

// Location resolves the project's timezone; boot adapters call it for other modules.
func (s *Service) Location(ctx context.Context, orgID, projectID uuid.UUID) (*time.Location, error) {
	ctx, span := tracer.Start(ctx, "ledger.Location")
	defer span.End()

	st, err := s.effective(ctx, "ledger.Location", orgID, projectID)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	loc, err := st.Location()
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	return loc, nil
}

// StartDay returns the project's period start day-of-month; boot adapters call it for other modules.
func (s *Service) StartDay(ctx context.Context, orgID, projectID uuid.UUID) (int, error) {
	ctx, span := tracer.Start(ctx, "ledger.StartDay")
	defer span.End()

	st, err := s.effective(ctx, "ledger.StartDay", orgID, projectID)
	if err != nil {
		span.RecordError(err)
		return 0, err
	}
	return st.PeriodStartDay, nil
}

// DefaultCurrency returns the project's default currency; boot adapters call it for other modules.
func (s *Service) DefaultCurrency(ctx context.Context, orgID, projectID uuid.UUID) (money.Currency, error) {
	ctx, span := tracer.Start(ctx, "ledger.DefaultCurrency")
	defer span.End()

	st, err := s.effective(ctx, "ledger.DefaultCurrency", orgID, projectID)
	if err != nil {
		span.RecordError(err)
		return "", err
	}
	return st.Currency, nil
}

func (s *Service) effective(ctx context.Context, op string, orgID, projectID uuid.UUID) (*Settings, error) {
	st, err := s.store.ByProject(ctx, orgID, projectID)
	if err != nil {
		if IsNotFoundError(err) {
			return Defaults(orgID, projectID), nil
		}
		return nil, s.unexpected(ctx, op+": byProject", err,
			"org_id", orgID, "project_id", projectID)
	}
	return st, nil
}
