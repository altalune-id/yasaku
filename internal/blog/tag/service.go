package tag

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
var tracer = otel.Tracer("altalune.id/yasaku/internal/blog/tag")

// Service is the tags driving port.
type Service struct {
	store      Store
	log        *slog.Logger
	unexpected apperror.UnexpectedFunc
}

// NewService binds the service to its dependencies.
func NewService(store Store, log *slog.Logger, unexpected apperror.UnexpectedFunc) *Service {
	return &Service{store: store, log: log.With("module", "blog.tag"), unexpected: unexpected}
}

// Create constructs a Tag in the caller's tenant scope and persists it.
func (s *Service) Create(ctx context.Context, name, slug string) (*Tag, error) {
	ctx, span := tracer.Start(ctx, "tag.Create")
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	span.SetAttributes(
		attribute.String("org_id", tc.OrgID.String()),
		attribute.String("project_id", tc.ProjectID.String()),
	)

	t, err := New(tc.OrgID, tc.ProjectID, name, slug)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	if saveErr := s.store.Save(ctx, t); saveErr != nil {
		span.RecordError(saveErr)
		if IsAlreadyExistsError(saveErr) {
			return nil, saveErr
		}
		return nil, s.unexpected(ctx, "tag.Create: save", saveErr,
			"org_id", tc.OrgID, "project_id", tc.ProjectID)
	}
	span.SetAttributes(attribute.String("tag.id", t.ID.String()))
	return t, nil
}

// EnsureByName returns the project's tag with that name, creating it when absent.
func (s *Service) EnsureByName(ctx context.Context, orgID, projectID uuid.UUID, name string) (*Tag, error) {
	ctx, span := tracer.Start(ctx, "tag.EnsureByName")
	defer span.End()

	t, err := New(orgID, projectID, name, "")
	if err != nil {
		return nil, err
	}
	existing, err := s.store.BySlug(ctx, orgID, projectID, t.Slug)
	if err == nil {
		return existing, nil
	}
	if !IsNotFoundError(err) {
		return nil, s.unexpected(ctx, "tag.EnsureByName: lookup by slug", err)
	}
	if saveErr := s.store.Save(ctx, t); saveErr != nil {
		// NOTE: a concurrent create loses the race; re-read rather than surfacing AlreadyExists to the caller.
		if IsAlreadyExistsError(saveErr) {
			return s.store.BySlug(ctx, orgID, projectID, t.Slug)
		}
		return nil, s.unexpected(ctx, "tag.EnsureByName: save", saveErr)
	}
	return t, nil
}

// Rename replaces a tag's display name, leaving its slug intact.
func (s *Service) Rename(ctx context.Context, id uuid.UUID, name string) (*Tag, error) {
	ctx, span := tracer.Start(ctx, "tag.Rename",
		trace.WithAttributes(attribute.String("tag.id", id.String())))
	defer span.End()

	t, err := s.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := t.Rename(name); err != nil {
		span.RecordError(err)
		return nil, err
	}
	if saveErr := s.store.Save(ctx, t); saveErr != nil {
		span.RecordError(saveErr)
		if IsAlreadyExistsError(saveErr) {
			return nil, saveErr
		}
		return nil, s.unexpected(ctx, "tag.Rename: save", saveErr, "tag_id", id)
	}
	return t, nil
}

// List returns the tags in the caller's tenant scope.
func (s *Service) List(ctx context.Context) ([]*Tag, error) {
	ctx, span := tracer.Start(ctx, "tag.List")
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
		return nil, s.unexpected(ctx, "tag.List: list", err,
			"org_id", tc.OrgID, "project_id", tc.ProjectID)
	}
	return out, nil
}

// ByID returns the identified tag when it belongs to the caller's tenant scope.
func (s *Service) ByID(ctx context.Context, id uuid.UUID) (*Tag, error) {
	ctx, span := tracer.Start(ctx, "tag.ByID",
		trace.WithAttributes(attribute.String("tag.id", id.String())))
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	t, err := s.store.ByID(ctx, id)
	if err != nil {
		span.RecordError(err)
		if IsNotFoundError(err) {
			return nil, err
		}
		return nil, s.unexpected(ctx, "tag.ByID: load", err, "tag_id", id)
	}
	// SECURITY: the store scopes by org, not by project. Without this a tag id from a sibling
	// project in the same org would resolve. NotFound, not a distinct error, so the caller
	// learns nothing about rows outside its scope.
	if t.OrgID != tc.OrgID || t.ProjectID != tc.ProjectID {
		return nil, &NotFoundError{ID: id.String()}
	}
	return t, nil
}

// Delete removes a tag, refusing while posts still reference it.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	ctx, span := tracer.Start(ctx, "tag.Delete",
		trace.WithAttributes(attribute.String("tag.id", id.String())))
	defer span.End()

	if _, err := s.ByID(ctx, id); err != nil {
		return err
	}
	if err := s.store.Delete(ctx, id); err != nil {
		span.RecordError(err)
		if IsNotFoundError(err) || IsInUseError(err) {
			return err
		}
		return s.unexpected(ctx, "tag.Delete: delete", err, "tag_id", id)
	}
	return nil
}
