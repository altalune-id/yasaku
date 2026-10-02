package blog

import (
	"cmp"
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/platform/events"
	"altalune.id/yasaku/internal/platform/tenant"
)

//nolint:gochecknoglobals // OTel tracer is a package-level fixture, not runtime state.
var tracer = otel.Tracer("altalune.id/yasaku/internal/blog")

// Webhooks is the port a post transition enqueues its outbound event through, inside the write's unit of work.
type Webhooks interface {
	Enqueue(ctx context.Context, t events.Type, data any) error
}

// Service is the posts driving port.
type Service struct {
	store      Store
	log        *slog.Logger
	unexpected apperror.UnexpectedFunc
	uow        tenant.UnitOfWork
	hooks      Webhooks
}

// NewService binds the service to its dependencies.
func NewService(store Store, log *slog.Logger, unexpected apperror.UnexpectedFunc, uow tenant.UnitOfWork, hooks Webhooks) *Service {
	return &Service{store: store, log: log.With("module", "blog"), unexpected: unexpected, uow: uow, hooks: hooks}
}

// Locate returns one post in the caller's org, for callers whose authority is org-wide.
func (s *Service) Locate(ctx context.Context, id uuid.UUID) (*Post, error) {
	ctx, span := tracer.Start(ctx, "blog.Locate")
	defer span.End()
	span.SetAttributes(attribute.String("post.id", id.String()))

	_, p, err := s.locate(ctx, span, "blog.Locate", id)
	return p, err
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
	if saveErr := s.store.Save(ctx, p, 0); saveErr != nil {
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

// Update re-validates and replaces the editable fields of an existing post, leaving its tag set alone; ifVersion 0 writes unconditionally.
func (s *Service) Update(ctx context.Context, id uuid.UUID, title, slug, body string, categoryID uuid.UUID, ifVersion int) (*Post, error) {
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
	return s.persist(ctx, span, "blog.Update", p, ifVersion)
}

// UpdateWithTags replaces the editable fields and the tag set in one conditional write; ifVersion 0 writes unconditionally.
func (s *Service) UpdateWithTags(ctx context.Context, id uuid.UUID, title, slug, body string, categoryID uuid.UUID, tagIDs []uuid.UUID, ifVersion int) (*Post, error) {
	ctx, span := tracer.Start(ctx, "blog.UpdateWithTags")
	defer span.End()
	span.SetAttributes(attribute.String("post.id", id.String()), attribute.Int("tag.count", len(tagIDs)))

	p, err := s.load(ctx, span, "blog.UpdateWithTags", id)
	if err != nil {
		return nil, err
	}
	if updErr := p.Update(title, slug, body, categoryID); updErr != nil {
		span.RecordError(updErr)
		return nil, updErr
	}
	p.SetTags(tagIDs)
	return s.persist(ctx, span, "blog.UpdateWithTags", p, ifVersion)
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
	return s.persist(ctx, span, "blog.SetTags", p, 0)
}

// Publish marks the post published and emits PostPublished, recording the first publication once; ifVersion 0 guards on the loaded version.
func (s *Service) Publish(ctx context.Context, id uuid.UUID, ifVersion int) (*Post, error) {
	ctx, span := tracer.Start(ctx, "blog.Publish")
	defer span.End()
	span.SetAttributes(attribute.String("post.id", id.String()))

	return s.transition(ctx, span, "blog.Publish", id, ifVersion, StatusPublished)
}

// Unpublish returns the post to draft and emits PostUnpublished, retaining its first publication time; ifVersion 0 guards on the loaded version.
func (s *Service) Unpublish(ctx context.Context, id uuid.UUID, ifVersion int) (*Post, error) {
	ctx, span := tracer.Start(ctx, "blog.Unpublish")
	defer span.End()
	span.SetAttributes(attribute.String("post.id", id.String()))

	return s.transition(ctx, span, "blog.Unpublish", id, ifVersion, StatusDraft)
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

// BySlug returns one post in the caller's tenant scope by its slug.
func (s *Service) BySlug(ctx context.Context, slug string) (*Post, error) {
	ctx, span := tracer.Start(ctx, "blog.BySlug")
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return nil, err
	}
	span.SetAttributes(
		attribute.String("org_id", tc.OrgID.String()),
		attribute.String("project_id", tc.ProjectID.String()),
	)

	p, err := s.store.BySlug(ctx, tc.ProjectID, slug)
	if err != nil {
		span.RecordError(err)
		if IsNotFoundError(err) {
			return nil, err
		}
		return nil, s.unexpected(ctx, "blog.BySlug: load", err,
			"project_id", tc.ProjectID, "slug", slug)
	}
	return p, nil
}

// Delete removes a post together with its tag links and emits PostDeleted; ifVersion 0 guards on the loaded version.
func (s *Service) Delete(ctx context.Context, id uuid.UUID, ifVersion int) error {
	ctx, span := tracer.Start(ctx, "blog.Delete")
	defer span.End()
	span.SetAttributes(attribute.String("post.id", id.String()))

	const op = "blog.Delete"
	return s.atomically(ctx, span, op, id, ifVersion, func(ctx context.Context) error {
		p, err := s.load(ctx, span, op, id)
		if err != nil {
			return err
		}
		if delErr := s.store.Delete(ctx, id, cmp.Or(ifVersion, p.Version)); delErr != nil {
			span.RecordError(delErr)
			if IsNotFoundError(delErr) || IsStaleVersionError(delErr) {
				return delErr
			}
			return s.unexpected(ctx, op+": delete", delErr, "post_id", id)
		}
		return s.enqueue(ctx, span, op, id, events.PostDeleted, events.PostDeletedV1{
			ID:           p.ID,
			Slug:         p.Slug,
			WasPublished: p.Status == StatusPublished,
		})
	})
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
	tc, p, err := s.locate(ctx, span, op, id)
	if err != nil {
		return nil, err
	}
	if p.ProjectID != tc.ProjectID {
		err := &NotFoundError{ID: id.String()}
		span.RecordError(err)
		return nil, err
	}
	return p, nil
}

func (s *Service) locate(ctx context.Context, span trace.Span, op string, id uuid.UUID) (tenant.Context, *Post, error) {
	tc, err := tenant.From(ctx)
	if err != nil {
		span.RecordError(err)
		return tenant.Context{}, nil, err
	}
	p, err := s.store.ByID(ctx, id)
	if err != nil {
		span.RecordError(err)
		if IsNotFoundError(err) {
			return tenant.Context{}, nil, err
		}
		return tenant.Context{}, nil, s.unexpected(ctx, op+": load", err, "post_id", id)
	}
	if p.OrgID != tc.OrgID {
		err := &NotFoundError{ID: id.String()}
		span.RecordError(err)
		return tenant.Context{}, nil, err
	}
	return tc, p, nil
}

func (s *Service) transition(ctx context.Context, span trace.Span, op string, id uuid.UUID, ifVersion int, target Status) (*Post, error) {
	var out *Post
	err := s.atomically(ctx, span, op, id, ifVersion, func(ctx context.Context) error {
		p, err := s.load(ctx, span, op, id)
		if err != nil {
			return err
		}
		if ifVersion != 0 && ifVersion != p.Version {
			err := &StaleVersionError{Want: ifVersion, Got: p.Version}
			span.RecordError(err)
			return err
		}
		if p.Status == target {
			out = p
			return nil
		}
		effective := cmp.Or(ifVersion, p.Version)
		t := apply(p, target)
		if _, err := s.persist(ctx, span, op, p, effective); err != nil {
			return err
		}
		if err := s.enqueue(ctx, span, op, id, t, payload(t, p)); err != nil {
			return err
		}
		out = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) atomically(ctx context.Context, span trace.Span, op string, id uuid.UUID, ifVersion int, fn func(ctx context.Context) error) error {
	if _, err := tenant.From(ctx); err != nil {
		span.RecordError(err)
		return err
	}
	err := s.inUnitOfWork(ctx, span, op, id, fn)
	if ifVersion == 0 && IsStaleVersionError(err) {
		return s.inUnitOfWork(ctx, span, op, id, fn)
	}
	return err
}

func (s *Service) inUnitOfWork(ctx context.Context, span trace.Span, op string, id uuid.UUID, fn func(ctx context.Context) error) error {
	var inner error
	err := s.uow(ctx, func(ctx context.Context) error {
		inner = fn(ctx)
		return inner
	})
	if err == nil || inner != nil {
		return inner
	}
	span.RecordError(err)
	return s.unexpected(ctx, op+": unit of work", err, "post_id", id)
}

func (s *Service) enqueue(ctx context.Context, span trace.Span, op string, id uuid.UUID, t events.Type, data any) error {
	if err := s.hooks.Enqueue(ctx, t, data); err != nil {
		span.RecordError(err)
		return s.unexpected(ctx, op+": enqueue", err, "post_id", id)
	}
	return nil
}

func apply(p *Post, target Status) events.Type {
	if target == StatusPublished {
		p.Publish()
		return events.PostPublished
	}
	p.Unpublish()
	return events.PostUnpublished
}

func payload(t events.Type, p *Post) any {
	if t == events.PostPublished {
		return events.PostPublishedV1(snapshot(p))
	}
	return events.PostUnpublishedV1(snapshot(p))
}

func snapshot(p *Post) events.PostSnapshotV1 {
	var firstPublishedAt *time.Time
	if p.FirstPublishedAt != nil {
		t := p.FirstPublishedAt.UTC().Truncate(time.Second)
		firstPublishedAt = &t
	}
	return events.PostSnapshotV1{
		ID:               p.ID,
		Slug:             p.Slug,
		Title:            p.Title,
		BodyMarkdown:     p.BodyMarkdown,
		CategoryID:       p.CategoryID,
		TagIDs:           append([]uuid.UUID{}, p.TagIDs...),
		FirstPublishedAt: firstPublishedAt,
		UpdatedAt:        p.UpdatedAt.UTC().Truncate(time.Second),
		Version:          p.Version,
	}
}

func (s *Service) persist(ctx context.Context, span trace.Span, op string, p *Post, ifVersion int) (*Post, error) {
	if err := s.store.Save(ctx, p, ifVersion); err != nil {
		span.RecordError(err)
		if IsAlreadyExistsError(err) || IsNotFoundError(err) || IsStaleVersionError(err) {
			return nil, err
		}
		return nil, s.unexpected(ctx, op+": save", err, "post_id", p.ID)
	}
	p.Version++
	return p, nil
}
