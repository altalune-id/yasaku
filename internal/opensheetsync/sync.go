package opensheetsync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"strconv"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/platform/sealer"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/opensheet"
)

const (
	// NOTE: the lease outlives the 25s handler timeout, so two jobs never push one row at once.
	leaseTTL    = time.Minute
	maxPasses   = 3
	rowHeadroom = 10 * time.Second
	maxErrorLen = 512
	// NOTE: settle, release, the follow-up listing and the outcome run detached from the handler's deadline, so a push that used the whole budget still records what happened.
	bookkeepingTimeout = 3 * time.Second
)

// Syncer runs opensheet.sync: it leases each dirty row, pushes the row's current state and settles the version it pushed.
type Syncer struct {
	store      Store
	sealer     sealer.Sealer
	endpoint   Endpoint
	source     Source
	jobs       Queue
	queued     bool
	log        *slog.Logger
	unexpected apperror.UnexpectedFunc
	now        func() time.Time
}

// SyncerDeps is what a Syncer needs beyond its store, logger and reporter; Jobs takes follow-up jobs only when Queued, so an inline run never chains.
type SyncerDeps struct {
	Sealer   sealer.Sealer
	Endpoint Endpoint
	Source   Source
	Jobs     Queue
	Queued   bool
	Now      func() time.Time
}

// NewSyncer binds the sync job to its store, the sealed key, the opensheet endpoint and the source of current rows; a nil unexpected logs.
func NewSyncer(store Store, log *slog.Logger, unexpected apperror.UnexpectedFunc, d SyncerDeps) *Syncer {
	now := d.Now
	if now == nil {
		now = time.Now
	}
	log = log.With("module", "opensheetsync")
	if unexpected == nil {
		unexpected = apperror.NewReporter(log, false).Unexpected
	}
	return &Syncer{
		store: store, sealer: d.Sealer, endpoint: d.Endpoint, source: d.Source, jobs: d.Jobs, queued: d.Queued,
		log: log, unexpected: unexpected, now: now,
	}
}

// Sync pushes the current state of refs in projectID; *SyncRefusedError and *ScopeMismatchError are permanent, a refused row is recorded and skipped, and any other error is worth a retry.
func (s *Syncer) Sync(ctx context.Context, projectID uuid.UUID, refs []Ref) error {
	ctx, span := tracer.Start(ctx, "opensheetsync.Sync")
	defer span.End()
	span.SetAttributes(attribute.Int("opensheetsync.refs", len(refs)))

	tc, err := tenant.From(ctx)
	if err != nil || tc.OrgID == uuid.Nil {
		return &ScopeMismatchError{Want: projectID.String()}
	}
	// SECURITY: the tenant comes from the job's headers; a payload naming another project is refused, never re-scoped.
	if tc.ProjectID != projectID {
		return &ScopeMismatchError{Want: projectID.String(), Got: tc.ProjectID.String()}
	}
	l, err := s.store.LinkByProject(ctx, tc.OrgID, tc.ProjectID)
	if IsLinkNotFoundError(err) {
		return nil
	}
	if err != nil {
		return s.fail(ctx, "opensheetsync.Sync: link", err, "project_id", projectID)
	}
	if !l.Enabled {
		return nil
	}
	key, err := openKey(s.sealer, l)
	if err != nil {
		return s.refuse(ctx, l, &SyncRefusedError{Code: "key", Cause: &KeyUnreadableError{Cause: err}})
	}
	c, err := s.endpoint.client(l.OSOrg, l.OSProject, key)
	if err != nil {
		return s.refuse(ctx, l, &SyncRefusedError{Code: "config", Cause: err})
	}
	refs = Dedupe(refs)
	pushed, done := 0, 0
	for _, r := range refs {
		if !rowBudget(ctx) {
			break
		}
		n, err := s.syncRow(ctx, c, l, r)
		pushed += n
		done++
		switch {
		case err == nil, IsRowRefusedError(err):
			continue
		case IsSyncRefusedError(err):
			return s.refuse(ctx, l, err)
		}
		return err
	}
	span.SetAttributes(attribute.Int("opensheetsync.pushed", pushed))
	if pushed > 0 {
		s.outcome(ctx, l, "")
	}
	// NOTE: one follow-up per full batch that made progress drains a backlog page by page without fan-out; a short page or no push ends the chain.
	if s.queued && len(refs) >= maxRefsPerJob && done == len(refs) && pushed > 0 {
		s.followUp(ctx, l)
	}
	return nil
}

// NOTE: a mark during the push leaves the row dirty after its settle, so the holder pushes again, at most maxPasses times.
func (s *Syncer) syncRow(ctx context.Context, c *opensheet.Client, l *Link, ref Ref) (int, error) {
	pushed := 0
	for range maxPasses {
		token := uuid.New()
		st, ok, err := s.store.Claim(ctx, l.OrgID, l.ProjectID, ref, token, s.now(), leaseTTL)
		if err != nil {
			return pushed, s.fail(ctx, "opensheetsync.Sync: claim", err, "entity", ref.Entity, "entity_id", ref.ID)
		}
		if !ok {
			return pushed, nil
		}
		if err := s.push(ctx, c, l, st); err != nil {
			s.release(ctx, l, st, token, err)
			return pushed, err
		}
		bctx, cancel := bookkeeping(ctx)
		settled, dirty, err := s.store.Settle(bctx, l.OrgID, l.ProjectID, ref, st.Version, token)
		cancel()
		if err != nil {
			return pushed, s.fail(ctx, "opensheetsync.Sync: settle", err, "entity", ref.Entity, "entity_id", ref.ID)
		}
		if settled {
			pushed++
		}
		if !settled || !dirty {
			return pushed, nil
		}
	}
	return pushed, nil
}

func (s *Syncer) release(ctx context.Context, l *Link, st State, token uuid.UUID, cause error) {
	level := slog.LevelDebug
	if IsRowRefusedError(cause) {
		level = slog.LevelWarn
	}
	s.log.Log(ctx, level, "opensheetsync: row not synced", slog.String("project_id", l.ProjectID.String()),
		slog.String("entity", string(st.Entity)), slog.String("entity_id", st.ID.String()), slog.String("err", cause.Error()))
	now := s.now()
	f := Failure{Reason: truncate(failureText(cause)), At: now, Version: st.Version}
	if IsRowRefusedError(cause) {
		f.Count, f.RetryAfter = true, now.Add(RowBackoff(st.Attempts+1))
	}
	bctx, cancel := bookkeeping(ctx)
	defer cancel()
	if err := s.store.Release(bctx, l.OrgID, l.ProjectID, st.Ref, token, f); err != nil {
		_ = s.fail(ctx, "opensheetsync.Sync: release", err, "entity", st.Entity, "entity_id", st.ID)
	}
}

func (s *Syncer) push(ctx context.Context, c *opensheet.Client, l *Link, st State) error {
	sheet, id := l.Sheets.For(st.Entity), st.ID.String()
	if st.Deleted {
		return remove(ctx, c, sheet, id)
	}
	row, found, err := s.rowOf(ctx, st.Ref)
	if err != nil {
		return err
	}
	if !found {
		return remove(ctx, c, sheet, id)
	}
	return upsert(ctx, c, sheet, id, st.Version, row)
}

func (s *Syncer) rowOf(ctx context.Context, ref Ref) (row map[string]string, found bool, err error) {
	switch ref.Entity {
	case EntityTransaction:
		var f TransactionFacts
		f, found, err = s.source.Transaction(ctx, ref.ID)
		row = TransactionRow(f)
	case EntityWallet:
		var f WalletFacts
		f, found, err = s.source.Wallet(ctx, ref.ID)
		row = WalletRow(f)
	case EntityCategory:
		var f CategoryFacts
		f, found, err = s.source.Category(ctx, ref.ID)
		row = CategoryRow(f)
	default:
		return nil, false, fmt.Errorf("opensheetsync: sync: unknown entity %q", ref.Entity)
	}
	if err != nil {
		if _, ok := apperror.AsAppError(err); ok {
			return nil, false, err
		}
		return nil, false, s.fail(ctx, "opensheetsync.Sync: source", err, "entity", ref.Entity, "entity_id", ref.ID)
	}
	return row, found, nil
}

// NOTE: the PATCH body never carries id (opensheet refuses it) nor deleted_at, so a row the sheet tombstoned takes the edit and stays tombstoned.
func upsert(ctx context.Context, c *opensheet.Client, sheet, id string, version int64, row map[string]string) error {
	patch := maps.Clone(row)
	delete(patch, "id")
	_, _, err := c.PatchRow(ctx, sheet, id, opensheet.Row(patch))
	if err == nil {
		return nil
	}
	if !rowMissing(err) {
		return refusal(sheet, err)
	}
	_, _, err = c.CreateRow(ctx, sheet, opensheet.Row(row), opensheet.WithIdempotencyKey(id+":"+strconv.FormatInt(version, 10)))
	if err == nil {
		return nil
	}
	if !rowTaken(err) {
		return refusal(sheet, err)
	}
	_, _, err = c.PatchRow(ctx, sheet, id, opensheet.Row(patch))
	// NOTE: SHT013 here means the row left the tab by hand between the create and this patch; nothing is left to update.
	if err == nil || rowMissing(err) {
		return nil
	}
	return refusal(sheet, err)
}

func remove(ctx context.Context, c *opensheet.Client, sheet, id string) error {
	err := c.DeleteRow(ctx, sheet, id)
	if err == nil || rowMissing(err) {
		return nil
	}
	return refusal(sheet, err)
}

func (s *Syncer) followUp(ctx context.Context, l *Link) {
	bctx, cancel := bookkeeping(ctx)
	defer cancel()
	now := s.now()
	refs, err := s.store.ListDirty(bctx, l.OrgID, l.ProjectID, now.Add(time.Second), now, maxRefsPerJob)
	if err != nil {
		_ = s.fail(ctx, "opensheetsync.Sync: follow-up", err, "project_id", l.ProjectID)
		return
	}
	if len(refs) == 0 {
		return
	}
	sctx, scancel := bookkeeping(ctx)
	defer scancel()
	if err := s.jobs.Submit(sctx, syncJob(), payloadOf(l.ProjectID, refs)); err != nil {
		_ = s.unexpected(ctx, "opensheetsync.Sync: follow-up submit", err, "project_id", l.ProjectID)
	}
}

func (s *Syncer) refuse(ctx context.Context, l *Link, err error) error {
	s.log.WarnContext(ctx, "opensheetsync: sync refused", slog.String("project_id", l.ProjectID.String()), slog.String("err", err.Error()))
	s.outcome(ctx, l, refusalText(err))
	return err
}

func (s *Syncer) outcome(ctx context.Context, l *Link, msg string) {
	bctx, cancel := bookkeeping(ctx)
	defer cancel()
	disabled, err := s.store.SaveOutcome(bctx, l.OrgID, l.ProjectID, Outcome{At: s.now(), Err: truncate(msg), LinkUpdatedAt: l.UpdatedAt})
	if err != nil {
		_ = s.fail(ctx, "opensheetsync.Sync: outcome", err, "project_id", l.ProjectID)
		return
	}
	if disabled {
		s.log.WarnContext(ctx, "opensheetsync: link turned off after repeated failures", slog.String("project_id", l.ProjectID.String()))
	}
}

// NOTE: a deadline or a cancellation is the job running out of time, not a fault; it is returned for a retry, never reported.
func (s *Syncer) fail(ctx context.Context, op string, err error, kv ...any) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return err
	}
	return s.unexpected(ctx, op, err, kv...)
}

func bookkeeping(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), bookkeepingTimeout)
}

func rowBudget(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	deadline, ok := ctx.Deadline()
	return !ok || time.Until(deadline) > rowHeadroom
}

func truncate(s string) string {
	if len(s) <= maxErrorLen {
		return s
	}
	return s[:maxErrorLen]
}
