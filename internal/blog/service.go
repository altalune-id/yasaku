package blog

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
var tracer = otel.Tracer("altalune.id/yasaku/internal/blog")

// Service is the posts driving port.
type Service struct {
	store      Store
	log        *slog.Logger
	unexpected apperror.UnexpectedFunc
}

// NewService binds the service to its dependencies.
func NewService(store Store, log *slog.Logger, unexpected apperror.UnexpectedFunc) *Service {
	return &Service{store: store, log: log.With("module", "blog"), unexpected: unexpected}
}

// Create constructs a draft post in the caller's tenant scope and persists it.
func (s *Service) Create(ctx context.Context, categoryID uuid.UUID, title, slug, body string) (*Post, error) {
	ctx, span := tracer.Start(ctx, "blog.Create")
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	span.SetAttributes(
		attribute.String("org_id", tc.OrgID.String()),
		attribute.String("project_id", tc.ProjectID.String()),
	)

	p, err := New(tc.OrgID, tc.ProjectID, categoryID, title, slug, body)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	if saveErr := s.store.Save(ctx, p); saveErr != nil {
		span.RecordError(saveErr)
		if IsAlreadyExistsError(saveErr) {
			return nil, saveErr
		}
		return nil, s.unexpected(ctx, "blog.Create: save", saveErr,
			"org_id", tc.OrgID, "project_id", tc.ProjectID)
	}
	span.SetAttributes(attribute.String("post.id", p.ID.String()))
	return p, nil
}

// Update re-validates and replaces the editable fields of an existing post.
func (s *Service) Update(ctx context.Context, id uuid.UUID, title, slug, body string, categoryID uuid.UUID) (*Post, error) {
	ctx, span := tracer.Start(ctx, "blog.Update")
	defer span.End()
	span.SetAttributes(attribute.String("post.id", id.String()))

	p, err := s.load(ctx, span, "blog.Update", id)
	if err != nil {
		return nil, err
	}
	if updErr := p.Update(title, slug, body, categoryID); updErr != nil {
		span.RecordError(updErr)
		return nil, updErr
	}
	return s.persist(ctx, span, "blog.Update", p)
}

// SetTags replaces the post's tag set.
func (s *Service) SetTags(ctx context.Context, id uuid.UUID, tagIDs []uuid.UUID) (*Post, error) {
	ctx, span := tracer.Start(ctx, "blog.SetTags")
	defer span.End()
	span.SetAttributes(attribute.String("post.id", id.String()), attribute.Int("tag.count", len(tagIDs)))

	p, err := s.load(ctx, span, "blog.SetTags", id)
	if err != nil {
		return nil, err
	}
	p.SetTags(tagIDs)
	return s.persist(ctx, span, "blog.SetTags", p)
}

// Publish marks the post published, recording the first publication only once.
func (s *Service) Publish(ctx context.Context, id uuid.UUID) (*Post, error) {
	ctx, span := tracer.Start(ctx, "blog.Publish")
	defer span.End()
	span.SetAttributes(attribute.String("post.id", id.String()))

	p, err := s.load(ctx, span, "blog.Publish", id)
	if err != nil {
		return nil, err
	}
	p.Publish()
	return s.persist(ctx, span, "blog.Publish", p)
}

// Unpublish returns the post to draft, retaining its first publication time.
func (s *Service) Unpublish(ctx context.Context, id uuid.UUID) (*Post, error) {
	ctx, span := tracer.Start(ctx, "blog.Unpublish")
	defer span.End()
	span.SetAttributes(attribute.String("post.id", id.String()))

	p, err := s.load(ctx, span, "blog.Unpublish", id)
	if err != nil {
		return nil, err
	}
	p.Unpublish()
	return s.persist(ctx, span, "blog.Unpublish", p)
}

// List returns the posts in the caller's tenant scope, filtered by opts.
func (s *Service) List(ctx context.Context, opts ListOpts) ([]*Post, error) {
	ctx, span := tracer.Start(ctx, "blog.List")
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	span.SetAttributes(
		attribute.String("org_id", tc.OrgID.String()),
		attribute.String("project_id", tc.ProjectID.String()),
	)

	out, err := s.store.List(ctx, tc.OrgID, tc.ProjectID, opts)
	if err != nil {
		span.RecordError(err)
		return nil, s.unexpected(ctx, "blog.List: list", err,
			"org_id", tc.OrgID, "project_id", tc.ProjectID)
	}
	return out, nil
}

// ByID returns one post in the caller's tenant scope.
func (s *Service) ByID(ctx context.Context, id uuid.UUID) (*Post, error) {
	ctx, span := tracer.Start(ctx, "blog.ByID")
	defer span.End()
	span.SetAttributes(attribute.String("post.id", id.String()))

	return s.load(ctx, span, "blog.ByID", id)
}

// Delete removes a post together with its tag links.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	ctx, span := tracer.Start(ctx, "blog.Delete")
	defer span.End()
	span.SetAttributes(attribute.String("post.id", id.String()))

	if err := s.store.Delete(ctx, id); err != nil {
		span.RecordError(err)
		if IsNotFoundError(err) {
			return err
		}
		return s.unexpected(ctx, "blog.Delete: delete", err, "post_id", id)
	}
	return nil
}

// CountByCategory returns the project's post count per category; a category with no posts is absent.
func (s *Service) CountByCategory(ctx context.Context) (map[uuid.UUID]int, error) {
	ctx, span := tracer.Start(ctx, "blog.CountByCategory")
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	out, err := s.store.CountByCategory(ctx, tc.OrgID, tc.ProjectID)
	if err != nil {
		span.RecordError(err)
		return nil, s.unexpected(ctx, "blog.CountByCategory: count", err,
			"org_id", tc.OrgID, "project_id", tc.ProjectID)
	}
	return out, nil
}

// CountByTag returns the project's post count per tag; a tag with no posts is absent.
func (s *Service) CountByTag(ctx context.Context) (map[uuid.UUID]int, error) {
	ctx, span := tracer.Start(ctx, "blog.CountByTag")
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	out, err := s.store.CountByTag(ctx, tc.OrgID, tc.ProjectID)
	if err != nil {
		span.RecordError(err)
		return nil, s.unexpected(ctx, "blog.CountByTag: count", err,
			"org_id", tc.OrgID, "project_id", tc.ProjectID)
	}
	return out, nil
}

func (s *Service) load(ctx context.Context, span trace.Span, op string, id uuid.UUID) (*Post, error) {
	p, err := s.store.ByID(ctx, id)
	if err != nil {
		span.RecordError(err)
		if IsNotFoundError(err) {
			return nil, err
		}
		return nil, s.unexpected(ctx, op+": load", err, "post_id", id)
	}
	return p, nil
}

func (s *Service) persist(ctx context.Context, span trace.Span, op string, p *Post) (*Post, error) {
	if err := s.store.Save(ctx, p); err != nil {
		span.RecordError(err)
		if IsAlreadyExistsError(err) || IsNotFoundError(err) {
			return nil, err
		}
		return nil, s.unexpected(ctx, op+": save", err, "post_id", p.ID)
	}
	return p, nil
}
