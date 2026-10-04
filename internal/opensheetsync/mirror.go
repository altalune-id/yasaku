package opensheetsync

import (
	"context"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/platform/queue"
	"altalune.id/yasaku/internal/platform/tenant"
)

//nolint:gochecknoglobals // OTel tracer is a package-level fixture, not runtime state.
var tracer = otel.Tracer("altalune.id/yasaku/internal/opensheetsync")

const (
	reconcileGrace = time.Minute
	// NOTE: with the queue off a tick lists up to two pages per project, because reconcileTimeout fits two 20s inline jobs per org; with it on, it sends one page and the sync job follows up page by page.
	reconcileInlineCap = 2 * maxRefsPerJob
	// NOTE: with the queue off a Submit runs inline for up to its 20s chain budget, detached from ctx, so a loop stops while one more still fits.
	submitHeadroom = 25 * time.Second
	// NOTE: a kick's reads run detached from the request, so only this bounds them; the submit keeps no caller deadline, the submitter bounds it.
	kickReadTimeout = 3 * time.Second
)

// Queue is the port sync jobs are submitted through; boot passes the yasaku job submitter, never Kernel.Queue.
type Queue interface {
	Submit(ctx context.Context, j queue.Job, data any) error
}

// Mirror marks rows dirty inside a write's unit of work and submits sync jobs after the commit.
type Mirror struct {
	store      Store
	jobs       Queue
	queued     bool
	log        *slog.Logger
	unexpected apperror.UnexpectedFunc
	now        func() time.Time
}

// NewMirror binds the mirror to its store and the job submitter; queued is whether jobs go to NATS rather than running inline, and a nil unexpected logs.
func NewMirror(store Store, jobs Queue, queued bool, log *slog.Logger, unexpected apperror.UnexpectedFunc, now func() time.Time) *Mirror {
	if now == nil {
		now = time.Now
	}
	log = log.With("module", "opensheetsync")
	if unexpected == nil {
		unexpected = apperror.NewReporter(log, false).Unexpected
	}
	return &Mirror{store: store, jobs: jobs, queued: queued, log: log, unexpected: unexpected, now: now}
}

// Mark re-marks the transactions showing a renamed wallet or category, then bumps each ref's version, when the project has an enabled link; it runs inside the caller's unit of work.
func (m *Mirror) Mark(ctx context.Context, refs ...Ref) error {
	if len(refs) == 0 {
		return nil
	}
	ctx, span := tracer.Start(ctx, "opensheetsync.Mark")
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return err
	}
	on, err := m.store.LinkEnabled(ctx, tc.OrgID, tc.ProjectID)
	if err != nil || !on {
		return err
	}
	refs = LockOrder(refs)
	span.SetAttributes(attribute.Int("opensheetsync.refs", len(refs)))
	now := m.now()
	for _, r := range refs {
		if !r.Cascade {
			continue
		}
		if _, err := m.store.MarkReferencing(ctx, tc.OrgID, tc.ProjectID, r, now); err != nil {
			return err
		}
	}
	return m.store.Mark(ctx, tc.OrgID, tc.ProjectID, refs, now)
}

// Kick submits one sync job for the first refs after the commit; the rest stay dirty for the follow-up job or the reconciler. It never fails the caller and submits nothing for a project without an enabled link.
func (m *Mirror) Kick(ctx context.Context, refs ...Ref) {
	if len(refs) == 0 {
		return
	}
	// NOTE: the write has committed, so a request that ended since still hands its job over.
	ctx, span := tracer.Start(context.WithoutCancel(ctx), "opensheetsync.Kick")
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		m.log.WarnContext(ctx, "opensheetsync: kick without a tenant", slog.String("err", err.Error()))
		return
	}
	rctx, cancel := context.WithTimeout(ctx, kickReadTimeout)
	on, err := m.store.LinkEnabled(rctx, tc.OrgID, tc.ProjectID)
	cancel()
	if err != nil {
		_ = m.unexpected(ctx, "opensheetsync.Kick: link", err, "project_id", tc.ProjectID)
		return
	}
	if !on {
		return
	}
	refs = LockOrder(refs)
	m.submit(ctx, tc.ProjectID, refs[:min(len(refs), maxRefsPerJob)])
}

// KickDirty submits one job for the project's oldest dirty rows, whatever their age; it follows a backfill.
func (m *Mirror) KickDirty(ctx context.Context) {
	ctx, span := tracer.Start(context.WithoutCancel(ctx), "opensheetsync.KickDirty")
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		m.log.WarnContext(ctx, "opensheetsync: kick without a tenant", slog.String("err", err.Error()))
		return
	}
	now := m.now()
	rctx, cancel := context.WithTimeout(ctx, kickReadTimeout)
	refs, err := m.store.ListDirty(rctx, tc.OrgID, tc.ProjectID, now.Add(time.Second), now, maxRefsPerJob)
	cancel()
	if err != nil {
		_ = m.unexpected(ctx, "opensheetsync.KickDirty: list dirty", err, "project_id", tc.ProjectID)
		return
	}
	m.submit(ctx, tc.ProjectID, refs)
}

// Reconcile re-submits, for every enabled link of the org in ctx, the rows still dirty a minute after their last mark: one page per project when queued, up to two pages per project inline, starting one link later each tick.
func (m *Mirror) Reconcile(ctx context.Context) (int, error) {
	ctx, span := tracer.Start(ctx, "opensheetsync.Reconcile")
	defer span.End()

	tc, err := tenant.From(ctx)
	if err != nil {
		return 0, err
	}
	links, err := m.store.ListEnabledLinks(ctx, tc.OrgID)
	if err != nil {
		return 0, m.unexpected(ctx, "opensheetsync.Reconcile: links", err, "org_id", tc.OrgID)
	}
	links = rotate(links, m.now())
	sent := 0
	for _, l := range links {
		if !hasHeadroom(ctx) {
			break
		}
		pctx := tenant.WithProject(ctx, l.ProjectID)
		now := m.now()
		limit := reconcileInlineCap
		if m.queued {
			limit = maxRefsPerJob
		}
		refs, err := m.store.ListDirty(pctx, tc.OrgID, l.ProjectID, now.Add(-reconcileGrace), now, limit)
		if err != nil {
			_ = m.unexpected(pctx, "opensheetsync.Reconcile: list dirty", err, "project_id", l.ProjectID)
			continue
		}
		sent += m.submit(pctx, l.ProjectID, refs)
	}
	span.SetAttributes(attribute.Int("opensheetsync.resubmitted", sent))
	return sent, nil
}

func (m *Mirror) submit(ctx context.Context, projectID uuid.UUID, refs []Ref) int {
	sent := 0
	for chunk := range slices.Chunk(refs, maxRefsPerJob) {
		if !hasHeadroom(ctx) {
			return sent
		}
		// NOTE: detached from the caller's cancellation, so a request that ends right after its commit still hands the job over.
		if err := m.jobs.Submit(context.WithoutCancel(ctx), syncJob(), payloadOf(projectID, chunk)); err != nil {
			_ = m.unexpected(ctx, "opensheetsync: submit", err, "project_id", projectID)
			continue
		}
		sent += len(chunk)
	}
	return sent
}

// NOTE: each tick starts one link later, so a project with a long backlog cannot use up every inline tick while the others wait.
func rotate(links []*Link, now time.Time) []*Link {
	if len(links) < 2 {
		return links
	}
	k := int(now.Unix()/int64(reconcileEvery/time.Second)) % len(links)
	return slices.Concat(links[k:], links[:k])
}

func hasHeadroom(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	deadline, ok := ctx.Deadline()
	return !ok || time.Until(deadline) > submitHeadroom
}
