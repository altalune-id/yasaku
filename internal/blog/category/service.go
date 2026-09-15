package category

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/platform/tenant"
)

//nolint:gochecknoglobals // OTel tracer is a package-level fixture, not runtime state.
var tracer = otel.Tracer("altalune.id/yasaku/internal/blog/category")

// Service is the categories driving port.
type Service struct {
	store      Store
	log        *slog.Logger
	unexpected apperror.UnexpectedFunc
}

// NewService binds the service to its dependencies.
func NewService(store Store, log *slog.Logger, unexpected apperror.UnexpectedFunc) *Service {
	return &Service{store: store, log: log.With("module", "blog.category"), unexpected: unexpected}
}

// Create constructs a Category in the caller's tenant scope and persists it.
func (s *Service) Create(ctx context.Context, name, slug string) (*Category, error) {
	ctx, span := tracer.Start(ctx, "category.Create")
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	span.SetAttributes(
		attribute.String("org_id", tc.OrgID.String()),
		attribute.String("project_id", tc.ProjectID.String()),
	)

	c, err := New(tc.OrgID, tc.ProjectID, name, slug)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	if err := s.store.Save(ctx, c); err != nil {
		span.RecordError(err)
		if IsAlreadyExistsError(err) {
			return nil, err
		}
		return nil, s.unexpected(ctx, "category.Create: save", err,
			"org_id", tc.OrgID, "project_id", tc.ProjectID)
	}
	span.SetAttributes(attribute.String("category.id", c.ID.String()))
	return c, nil
}

// Rename changes the display name of a category in the caller's tenant scope, leaving the slug alone.
func (s *Service) Rename(ctx context.Context, id uuid.UUID, name string) (*Category, error) {
	ctx, span := tracer.Start(ctx, "category.Rename",
		trace.WithAttributes(attribute.String("category.id", id.String())))
	defer span.End()

	c, err := s.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := c.Rename(name); err != nil {
		span.RecordError(err)
		return nil, err
	}
	if err := s.store.Save(ctx, c); err != nil {
		span.RecordError(err)
		if IsAlreadyExistsError(err) {
			return nil, err
		}
		return nil, s.unexpected(ctx, "category.Rename: save", err, "category_id", id)
	}
	return c, nil
}

// List returns the categories in the caller's tenant scope, newest first.
func (s *Service) List(ctx context.Context) ([]*Category, error) {
	ctx, span := tracer.Start(ctx, "category.List")
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	span.SetAttributes(
		attribute.String("org_id", tc.OrgID.String()),
		attribute.String("project_id", tc.ProjectID.String()),
	)

	out, err := s.store.List(ctx, tc.OrgID, tc.ProjectID)
	if err != nil {
		span.RecordError(err)
		return nil, s.unexpected(ctx, "category.List: list", err,
			"org_id", tc.OrgID, "project_id", tc.ProjectID)
	}
	return out, nil
}

// ByID returns the identified category when it belongs to the caller's tenant scope.
func (s *Service) ByID(ctx context.Context, id uuid.UUID) (*Category, error) {
	ctx, span := tracer.Start(ctx, "category.ByID",
		trace.WithAttributes(attribute.String("category.id", id.String())))
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	c, err := s.store.ByID(ctx, id)
	if err != nil {
		span.RecordError(err)
		if IsNotFoundError(err) {
			return nil, err
		}
		return nil, s.unexpected(ctx, "category.ByID: byID", err, "category_id", id)
	}
	if c.OrgID != tc.OrgID || c.ProjectID != tc.ProjectID {
		return nil, &NotFoundError{ID: id.String()}
	}
	return c, nil
}

// Delete removes the identified category; a category that still has posts is refused as InUseError.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	ctx, span := tracer.Start(ctx, "category.Delete",
		trace.WithAttributes(attribute.String("category.id", id.String())))
	defer span.End()

	if _, err := s.ByID(ctx, id); err != nil {
		return err
	}
	if err := s.store.Delete(ctx, id); err != nil {
		span.RecordError(err)
		if IsInUseError(err) || IsNotFoundError(err) {
			return err
		}
		return s.unexpected(ctx, "category.Delete: delete", err, "category_id", id)
	}
	return nil
}
