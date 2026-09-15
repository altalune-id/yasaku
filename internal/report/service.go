package report

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/money"
)

//nolint:gochecknoglobals // OTel tracer is a package-level fixture, not runtime state.
var tracer = otel.Tracer("altalune.id/yasaku/internal/report")

// Service is the reporting driving port; it is read-only and owns no Store.
type Service struct {
	reader     Reader
	log        *slog.Logger
	unexpected apperror.UnexpectedFunc
	settings   SettingsReader
}

// NewService binds the service to its dependencies.
func NewService(reader Reader, log *slog.Logger, unexpected apperror.UnexpectedFunc, settings SettingsReader) *Service {
	return &Service{
		reader:     reader,
		log:        log.With("module", "report"),
		unexpected: unexpected,
		settings:   settings,
	}
}

// Summary totals the identified period in the project's default currency.
func (s *Service) Summary(ctx context.Context, periodID uuid.UUID) (PeriodSummary, error) {
	ctx, span := tracer.Start(ctx, "report.Summary",
		trace.WithAttributes(attribute.String("period.id", periodID.String())))
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return PeriodSummary{}, err
	}
	span.SetAttributes(
		attribute.String("org_id", tc.OrgID.String()),
		attribute.String("project_id", tc.ProjectID.String()),
	)
	return s.summary(ctx, tc.OrgID, tc.ProjectID, periodID)
}

// SnapshotFor totals the identified period in an explicit scope; boot's period.Snapshotter adapter calls it.
func (s *Service) SnapshotFor(ctx context.Context, orgID, projectID, periodID uuid.UUID) (PeriodSummary, error) {
	ctx, span := tracer.Start(ctx, "report.SnapshotFor",
		trace.WithAttributes(attribute.String("period.id", periodID.String())))
	defer span.End()

	return s.summary(ctx, orgID, projectID, periodID)
}

// SpendByCategory breaks the identified period's expenses down by category, largest first.
func (s *Service) SpendByCategory(ctx context.Context, periodID uuid.UUID) ([]CategorySlice, error) {
	ctx, span := tracer.Start(ctx, "report.SpendByCategory",
		trace.WithAttributes(attribute.String("period.id", periodID.String())))
	defer span.End()

	tc, currency, err := s.scope(ctx)
	if err != nil {
		return nil, err
	}
	out, err := s.reader.SpendByCategory(ctx, tc.OrgID, tc.ProjectID, periodID, currency)
	if err != nil {
		span.RecordError(err)
		return nil, s.unexpected(ctx, "report.SpendByCategory: read", err, "period_id", periodID)
	}
	return out, nil
}

// IncomeByCategory breaks the identified period's income down by category, largest first.
func (s *Service) IncomeByCategory(ctx context.Context, periodID uuid.UUID) ([]CategorySlice, error) {
	ctx, span := tracer.Start(ctx, "report.IncomeByCategory",
		trace.WithAttributes(attribute.String("period.id", periodID.String())))
	defer span.End()

	tc, currency, err := s.scope(ctx)
	if err != nil {
		return nil, err
	}
	out, err := s.reader.IncomeByCategory(ctx, tc.OrgID, tc.ProjectID, periodID, currency)
	if err != nil {
		span.RecordError(err)
		return nil, s.unexpected(ctx, "report.IncomeByCategory: read", err, "period_id", periodID)
	}
	return out, nil
}

// Cashflow returns one point per requested period, in the order the ids were given.
func (s *Service) Cashflow(ctx context.Context, periodIDs []uuid.UUID) ([]CashflowPoint, error) {
	ctx, span := tracer.Start(ctx, "report.Cashflow",
		trace.WithAttributes(attribute.Int("report.period_count", len(periodIDs))))
	defer span.End()

	tc, currency, err := s.scope(ctx)
	if err != nil {
		return nil, err
	}
	out, err := s.reader.Cashflow(ctx, tc.OrgID, tc.ProjectID, periodIDs, currency)
	if err != nil {
		span.RecordError(err)
		return nil, s.unexpected(ctx, "report.Cashflow: read", err,
			"org_id", tc.OrgID, "project_id", tc.ProjectID)
	}
	return out, nil
}

// Flows returns the identified period's expenses grouped by wallet and category.
func (s *Service) Flows(ctx context.Context, periodID uuid.UUID) ([]Flow, error) {
	ctx, span := tracer.Start(ctx, "report.Flows",
		trace.WithAttributes(attribute.String("period.id", periodID.String())))
	defer span.End()

	tc, currency, err := s.scope(ctx)
	if err != nil {
		return nil, err
	}
	out, err := s.reader.Flows(ctx, tc.OrgID, tc.ProjectID, periodID, currency)
	if err != nil {
		span.RecordError(err)
		return nil, s.unexpected(ctx, "report.Flows: read", err, "period_id", periodID)
	}
	return out, nil
}

// WalletBalances returns every wallet's live balance in the caller's project.
func (s *Service) WalletBalances(ctx context.Context) ([]WalletLine, error) {
	ctx, span := tracer.Start(ctx, "report.WalletBalances")
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	out, err := s.reader.WalletBalances(ctx, tc.OrgID, tc.ProjectID)
	if err != nil {
		span.RecordError(err)
		return nil, s.unexpected(ctx, "report.WalletBalances: read", err,
			"org_id", tc.OrgID, "project_id", tc.ProjectID)
	}
	return out, nil
}

func (s *Service) summary(ctx context.Context, orgID, projectID, periodID uuid.UUID) (PeriodSummary, error) {
	currency, err := s.currency(ctx, orgID, projectID)
	if err != nil {
		return PeriodSummary{}, err
	}
	loc, err := s.location(ctx, orgID, projectID)
	if err != nil {
		return PeriodSummary{}, err
	}
	ref, err := s.reader.Period(ctx, orgID, projectID, periodID)
	if err != nil {
		return PeriodSummary{}, s.unexpected(ctx, "report.Summary: period", err, "period_id", periodID)
	}
	out, err := s.reader.Summary(ctx, orgID, projectID, periodID, currency, ref.Start.In(loc))
	if err != nil {
		return PeriodSummary{}, s.unexpected(ctx, "report.Summary: read", err, "period_id", periodID)
	}
	return out, nil
}

func (s *Service) scope(ctx context.Context) (tenant.Context, money.Currency, error) {
	tc, err := tenant.From(ctx)
	if err != nil {
		return tenant.Context{}, "", err
	}
	currency, err := s.currency(ctx, tc.OrgID, tc.ProjectID)
	if err != nil {
		return tenant.Context{}, "", err
	}
	return tc, currency, nil
}

func (s *Service) currency(ctx context.Context, orgID, projectID uuid.UUID) (money.Currency, error) {
	currency, err := s.settings.DefaultCurrency(ctx, orgID, projectID)
	if err != nil {
		return "", s.unexpected(ctx, "report: default currency", err,
			"org_id", orgID, "project_id", projectID)
	}
	return currency, nil
}

func (s *Service) location(ctx context.Context, orgID, projectID uuid.UUID) (*time.Location, error) {
	loc, err := s.settings.Location(ctx, orgID, projectID)
	if err != nil {
		return nil, s.unexpected(ctx, "report: location", err,
			"org_id", orgID, "project_id", projectID)
	}
	if loc == nil {
		return time.UTC, nil
	}
	return loc, nil
}
