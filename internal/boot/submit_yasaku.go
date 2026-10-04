package boot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/queue"
	"altalune.id/yasaku/internal/platform/tenant"
)

const (
	inlineBudget          = 20 * time.Second
	maxInlineDepth        = 2
	inlineReasonPermanent = "permanent"
	inlineReasonExhausted = "exhausted"
	inlineCauseMaxLen     = 1 << 10
)

type inlineDepthKey struct{}

type inlineDeadlineKey struct{}

// InlineJobError reports a job that ran inline, because the queue is disabled, and failed.
type InlineJobError struct {
	Job    queue.Job
	Reason string
	Cause  error
}

func (e *InlineJobError) Error() string {
	return "boot: inline job " + e.Job.Subject() + " " + e.Reason + ": " + e.Cause.Error()
}

func (e *InlineJobError) Unwrap() error { return e.Cause }

// IsInlineJobError reports whether err's tree contains an *InlineJobError.
func IsInlineJobError(err error) bool {
	_, ok := errors.AsType[*InlineJobError](err)
	return ok
}

// InlineDepthError reports an inline job submitted from inline jobs nested deeper than maxInlineDepth.
type InlineDepthError struct {
	Job   queue.Job
	Depth int
}

func (e *InlineDepthError) Error() string {
	return "boot: inline job " + e.Job.Subject() + " at depth " + strconv.Itoa(e.Depth) + " exceeds " + strconv.Itoa(maxInlineDepth)
}

// IsInlineDepthError reports whether err's tree contains an *InlineDepthError.
func IsInlineDepthError(err error) bool {
	_, ok := errors.AsType[*InlineDepthError](err)
	return ok
}

// SubmitInUnitOfWorkError reports a Submit made inside a unit of work, before the commit.
type SubmitInUnitOfWorkError struct{ Job queue.Job }

func (e *SubmitInUnitOfWorkError) Error() string {
	return "boot: submit " + e.Job.Subject() + " inside a unit of work; submit after the commit"
}

// IsSubmitInUnitOfWorkError reports whether err's tree contains a *SubmitInUnitOfWorkError.
func IsSubmitInUnitOfWorkError(err error) bool {
	_, ok := errors.AsType[*SubmitInUnitOfWorkError](err)
	return ok
}

type jobSubmitter struct {
	queue    *queue.Client
	tracer   trace.Tracer
	log      *slog.Logger
	handlers atomic.Pointer[map[queue.Job]queue.Handler]
}

func newJobSubmitter(q *queue.Client, tracer trace.Tracer, log *slog.Logger) *jobSubmitter {
	if tracer == nil {
		tracer = tracenoop.NewTracerProvider().Tracer("")
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &jobSubmitter{queue: q, tracer: tracer, log: log}
}

func (s *jobSubmitter) bind(hs []queue.Handler) {
	m := make(map[queue.Job]queue.Handler, len(hs))
	for _, h := range hs {
		m[h.Job] = h
	}
	s.handlers.Store(&m)
	if !s.queue.Enabled() {
		s.log.Info("queue: disabled — yasaku jobs run inline after the commit", slog.Int("jobs", len(m)))
	}
}

// Submit publishes j when the queue is enabled and runs its handler inline when it is not.
func (s *jobSubmitter) Submit(ctx context.Context, j queue.Job, data any) error {
	if _, inTx := db.CurrentTx(ctx); inTx {
		return &SubmitInUnitOfWorkError{Job: j}
	}
	if s.queue.Enabled() {
		return s.queue.Submit(ctx, j, data)
	}
	return s.runInline(ctx, j, data)
}

func (s *jobSubmitter) handler(j queue.Job) (queue.Handler, bool) {
	m := s.handlers.Load()
	if m == nil {
		return queue.Handler{}, false
	}
	h, ok := (*m)[j]
	return h, ok
}

func (s *jobSubmitter) runInline(ctx context.Context, j queue.Job, data any) error {
	depth := inlineDepth(ctx) + 1
	if depth > maxInlineDepth {
		return &InlineDepthError{Job: j, Depth: depth}
	}
	h, ok := s.handler(j)
	if !ok {
		return &queue.UndeclaredJobError{Job: j}
	}
	m, err := message(j, data)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(inlineBudget)
	if outer, ok := ctx.Value(inlineDeadlineKey{}).(time.Time); ok && outer.Before(deadline) {
		deadline = outer
	}
	if !deadline.After(time.Now()) {
		s.log.WarnContext(ctx, "queue: inline job skipped, chain budget spent", slog.String("subject", j.Subject()), slog.Int("depth", depth))
		return &InlineJobError{Job: j, Reason: inlineReasonExhausted, Cause: context.DeadlineExceeded}
	}
	hctx, cancel := context.WithDeadline(consumerContext(ctx, depth, deadline), deadline)
	defer cancel()
	hctx, span := s.tracer.Start(hctx, "queue.inline", trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(attribute.String("job", j.Name), attribute.String("subject", j.Subject()), attribute.Int("depth", depth)))
	defer span.End()

	herr := s.safely(hctx, j, func(ctx context.Context) error { return h.Handle(ctx, m) })
	if herr == nil {
		return nil
	}
	span.RecordError(herr)
	span.SetStatus(codes.Error, herr.Error())
	reason := inlineReasonExhausted
	if queue.IsPermanentError(herr) {
		reason = inlineReasonPermanent
	}
	if h.OnDeadLetter != nil {
		d := queue.DeadLetter{Message: m, Reason: reason, Cause: apperror.TruncateCause(herr.Error(), inlineCauseMaxLen)}
		if err := s.safely(hctx, j, func(ctx context.Context) error { return h.OnDeadLetter(ctx, d) }); err != nil {
			s.log.ErrorContext(hctx, "queue: inline OnDeadLetter", slog.String("subject", j.Subject()), slog.String("err", err.Error()))
		}
	}
	if h.SuppressReport {
		s.log.WarnContext(hctx, "queue: inline job failed", slog.String("subject", j.Subject()), slog.String("reason", reason), slog.String("err", herr.Error()))
		return nil
	}
	return &InlineJobError{Job: j, Reason: reason, Cause: herr}
}

func message(j queue.Job, data any) (queue.Message, error) {
	body, err := json.Marshal(data)
	if err != nil {
		return queue.Message{}, &queue.PublishError{Subject: j.Subject(), Cause: err}
	}
	id, err := uuid.NewV7()
	if err != nil {
		return queue.Message{}, &queue.PublishError{Subject: j.Subject(), Cause: err}
	}
	return queue.Message{ID: id, Name: j.Name, Version: j.Version, Data: body, NumDelivered: 1, CreatedAt: time.Now().UTC()}, nil
}

func (s *jobSubmitter) safely(ctx context.Context, j queue.Job, fn func(context.Context) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			s.log.ErrorContext(ctx, "queue: inline handler panicked", slog.String("subject", j.Subject()), slog.Any("panic", r), slog.String("stack", string(debug.Stack())))
			err = fmt.Errorf("queue: handler panicked: %v", r)
		}
	}()
	return fn(ctx)
}

func inlineDepth(ctx context.Context) int {
	d, _ := ctx.Value(inlineDepthKey{}).(int)
	return d
}

func consumerContext(ctx context.Context, depth int, deadline time.Time) context.Context {
	base := trace.ContextWithSpanContext(context.Background(), trace.SpanContextFromContext(ctx))
	base = context.WithValue(base, inlineDepthKey{}, depth)
	base = context.WithValue(base, inlineDeadlineKey{}, deadline)
	tc, err := tenant.From(ctx)
	if err != nil {
		return base
	}
	return tenant.Into(base, tc)
}
