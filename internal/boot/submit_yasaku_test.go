package boot

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"altalune.id/yasaku/internal/platform/capabilities"
	"altalune.id/yasaku/internal/platform/db"
	"altalune.id/yasaku/internal/platform/queue"
	"altalune.id/yasaku/internal/platform/tenant"
	"altalune.id/yasaku/reqid"
)

func echoJob() queue.Job { return queue.Job{Name: "yasakutest.echo", Version: 1} }

type echoV1 struct {
	Text string `json:"text"`
}

type echoRun struct {
	Msg      queue.Message
	Payload  echoV1
	Tenant   tenant.Context
	Scoped   bool
	ReqID    string
	CtxErr   error
	Deadline time.Time
	Span     trace.SpanContext
}

type echoJobs struct {
	mu        sync.Mutex
	runs      []echoRun
	dead      []queue.DeadLetter
	fail      func(n int) error
	suppress  bool
	ran       chan echoRun
	onDeadSet bool
}

func newEchoJobs() *echoJobs { return &echoJobs{ran: make(chan echoRun, 16)} }

func (e *echoJobs) ConsumerHandlers() []queue.Handler {
	h := queue.Handler{
		Job:            echoJob(),
		SuppressReport: e.suppress,
		Handle: func(ctx context.Context, m queue.Message) error {
			p, err := queue.Decode[echoV1](m)
			if err != nil {
				return queue.Permanent(err)
			}
			tc, terr := tenant.From(ctx)
			dl, _ := ctx.Deadline()
			r := echoRun{Msg: m, Payload: p, Tenant: tc, Scoped: terr == nil, ReqID: reqid.FromContext(ctx),
				CtxErr: ctx.Err(), Deadline: dl, Span: trace.SpanContextFromContext(ctx)}
			e.mu.Lock()
			e.runs = append(e.runs, r)
			e.mu.Unlock()
			select {
			case e.ran <- r:
			default:
			}
			if e.fail != nil {
				return e.fail(m.NumDelivered)
			}
			return nil
		},
	}
	if e.onDeadSet {
		h.OnDeadLetter = func(_ context.Context, d queue.DeadLetter) error {
			e.mu.Lock()
			defer e.mu.Unlock()
			e.dead = append(e.dead, d)
			return nil
		}
	}
	return []queue.Handler{h}
}

func (e *echoJobs) Runs() []echoRun {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]echoRun(nil), e.runs...)
}

func (e *echoJobs) DeadLetters() []queue.DeadLetter {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]queue.DeadLetter(nil), e.dead...)
}

func disabledSubmitter(t *testing.T, jobs *echoJobs) *jobSubmitter {
	t.Helper()
	q := queue.Disabled(discardLogger())
	hs := handlersOf([]queue.Provider{jobs})
	require.NoError(t, q.Declare(jobsOf(hs), nil))
	s := newJobSubmitter(q, nil, discardLogger())
	s.bind(hs)
	return s
}

func scopedCtx(ctx context.Context) (context.Context, tenant.Context) {
	tc := tenant.Context{OrgID: uuid.New(), ProjectID: uuid.New(), UserID: uuid.New()}
	return tenant.Into(ctx, tc), tc
}

func TestJobSubmitter_DisabledRunsTheHandlerInline(t *testing.T) {
	jobs := newEchoJobs()
	s := disabledSubmitter(t, jobs)

	require.NoError(t, s.Submit(t.Context(), echoJob(), echoV1{Text: "hi"}))

	runs := jobs.Runs()
	require.Len(t, runs, 1)
	r := runs[0]
	assert.Equal(t, echoV1{Text: "hi"}, r.Payload)
	assert.Equal(t, "yasakutest.echo", r.Msg.Name)
	assert.Equal(t, 1, r.Msg.Version)
	assert.Equal(t, 1, r.Msg.NumDelivered)
	assert.Equal(t, uuid.Version(7), r.Msg.ID.Version())
	assert.WithinDuration(t, time.Now(), r.Msg.CreatedAt, time.Minute)
}

func TestJobSubmitter_InlineHandlerSeesTheContextAConsumerWould(t *testing.T) {
	jobs := newEchoJobs()
	s := disabledSubmitter(t, jobs)
	ctx, tc := scopedCtx(reqid.WithContext(context.Background(), "req-1"))
	ctx, cancel := context.WithCancel(ctx)
	cancel()

	require.NoError(t, s.Submit(ctx, echoJob(), echoV1{}))

	r := jobs.Runs()[0]
	assert.True(t, r.Scoped)
	assert.Equal(t, tc, r.Tenant)
	assert.Empty(t, r.ReqID, "a consumer ctx carries no request values")
	require.NoError(t, r.CtxErr, "the handler is detached from the caller's cancellation")
	assert.WithinDuration(t, time.Now().Add(inlineBudget), r.Deadline, 5*time.Second)
}

func TestJobSubmitter_InlineUnscopedHasNoTenant(t *testing.T) {
	jobs := newEchoJobs()
	s := disabledSubmitter(t, jobs)

	require.NoError(t, s.Submit(t.Context(), echoJob(), echoV1{}))

	assert.False(t, jobs.Runs()[0].Scoped)
}

func TestJobSubmitter_InlineKeepsTheCallerTrace(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	jobs := newEchoJobs()
	s := disabledSubmitter(t, jobs)
	s.tracer = tp.Tracer("test")
	ctx, parent := tp.Tracer("test").Start(t.Context(), "request")

	require.NoError(t, s.Submit(ctx, echoJob(), echoV1{}))
	parent.End()

	r := jobs.Runs()[0]
	assert.Equal(t, parent.SpanContext().TraceID(), r.Span.TraceID())
	var inline sdktrace.ReadOnlySpan
	for _, sp := range sr.Ended() {
		if sp.Name() == "queue.inline" {
			inline = sp
		}
	}
	require.NotNil(t, inline)
	assert.Equal(t, parent.SpanContext().SpanID(), inline.Parent().SpanID())
}

func TestJobSubmitter_InlineErrors(t *testing.T) {
	flaky := errors.New("flaky")
	tests := []struct {
		name       string
		fail       func(int) error
		wantReason string
		permanent  bool
	}{
		{"transient", func(int) error { return flaky }, "exhausted", false},
		{"permanent", func(int) error { return queue.Permanent(flaky) }, "permanent", true},
		{"panic", func(int) error { panic("boom") }, "exhausted", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jobs := newEchoJobs()
			jobs.fail = tt.fail
			jobs.onDeadSet = true
			s := disabledSubmitter(t, jobs)

			err := s.Submit(t.Context(), echoJob(), echoV1{})

			require.True(t, IsInlineJobError(err), "got %v", err)
			ie, _ := errors.AsType[*InlineJobError](err)
			assert.Equal(t, tt.wantReason, ie.Reason)
			assert.Equal(t, tt.permanent, queue.IsPermanentError(err))
			require.Len(t, jobs.Runs(), 1, "inline runs once, with no retry")
			dead := jobs.DeadLetters()
			require.Len(t, dead, 1)
			assert.Equal(t, tt.wantReason, dead[0].Reason)
		})
	}
}

func TestJobSubmitter_InlineSuppressReportReturnsNil(t *testing.T) {
	jobs := newEchoJobs()
	jobs.suppress = true
	jobs.fail = func(int) error { return queue.Permanent(errors.New("expected")) }
	s := disabledSubmitter(t, jobs)

	require.NoError(t, s.Submit(t.Context(), echoJob(), echoV1{}))
	require.Len(t, jobs.Runs(), 1)
}

func TestJobSubmitter_RefusesAnUndeclaredJob(t *testing.T) {
	s := disabledSubmitter(t, newEchoJobs())
	err := s.Submit(t.Context(), queue.Job{Name: "yasakutest.other", Version: 1}, echoV1{})
	require.True(t, queue.IsUndeclaredJobError(err), "got %v", err)

	unbound := newJobSubmitter(queue.Disabled(discardLogger()), nil, nil)
	require.True(t, queue.IsUndeclaredJobError(unbound.Submit(t.Context(), echoJob(), echoV1{})))
}

func TestJobSubmitter_UnencodablePayloadIsAPublishError(t *testing.T) {
	jobs := newEchoJobs()
	s := disabledSubmitter(t, jobs)

	err := s.Submit(t.Context(), echoJob(), make(chan int))

	require.True(t, queue.IsPublishError(err), "got %v", err)
	require.Empty(t, jobs.Runs())
}

func TestJobSubmitter_RefusesASubmitInsideAUnitOfWork(t *testing.T) {
	jobs := newEchoJobs()
	s := disabledSubmitter(t, jobs)

	err := s.Submit(db.ContextWithTx(t.Context(), nil), echoJob(), echoV1{})

	require.True(t, IsSubmitInUnitOfWorkError(err), "got %v", err)
	require.Empty(t, jobs.Runs())
}

func chainJob() queue.Job { return queue.Job{Name: "yasakutest.chain", Version: 1} }

type chainV1 struct {
	Left int `json:"left"`
}

type chainJobs struct {
	s      *jobSubmitter
	mu     sync.Mutex
	depths []int
	errs   []error
}

func (c *chainJobs) ConsumerHandlers() []queue.Handler {
	return []queue.Handler{{
		Job: chainJob(),
		Handle: func(ctx context.Context, m queue.Message) error {
			p, err := queue.Decode[chainV1](m)
			if err != nil {
				return queue.Permanent(err)
			}
			c.mu.Lock()
			c.depths = append(c.depths, inlineDepth(ctx))
			c.mu.Unlock()
			if p.Left == 0 {
				return nil
			}
			if err := c.s.Submit(ctx, chainJob(), chainV1{Left: p.Left - 1}); err != nil {
				c.mu.Lock()
				c.errs = append(c.errs, err)
				c.mu.Unlock()
			}
			return nil
		},
	}}
}

func TestJobSubmitter_InlineAllowsOneLevelOfChaining(t *testing.T) {
	tests := []struct {
		name       string
		left       int
		wantDepths []int
		wantRefuse bool
	}{
		{"one job", 0, []int{1}, false},
		{"one chained job", 1, []int{1, 2}, false},
		{"a third level is refused", 5, []int{1, 2}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := queue.Disabled(discardLogger())
			s := newJobSubmitter(q, nil, discardLogger())
			chain := &chainJobs{s: s}
			hs := handlersOf([]queue.Provider{chain})
			require.NoError(t, q.Declare(jobsOf(hs), nil))
			s.bind(hs)

			require.NoError(t, s.Submit(t.Context(), chainJob(), chainV1{Left: tt.left}))

			assert.Equal(t, tt.wantDepths, chain.depths)
			if !tt.wantRefuse {
				assert.Empty(t, chain.errs)
				return
			}
			require.Len(t, chain.errs, 1)
			require.True(t, IsInlineDepthError(chain.errs[0]), "got %v", chain.errs[0])
			de, _ := errors.AsType[*InlineDepthError](chain.errs[0])
			assert.Equal(t, maxInlineDepth+1, de.Depth)
		})
	}
}

type nestJobs struct {
	s         *jobSubmitter
	outerDL   time.Time
	nestedDL  time.Time
	nestedRan bool
}

func (n *nestJobs) ConsumerHandlers() []queue.Handler {
	return []queue.Handler{{
		Job: chainJob(),
		Handle: func(ctx context.Context, m queue.Message) error {
			p, err := queue.Decode[chainV1](m)
			if err != nil {
				return queue.Permanent(err)
			}
			dl, _ := ctx.Deadline()
			if p.Left == 1 {
				n.outerDL = dl
				time.Sleep(50 * time.Millisecond)
				return n.s.Submit(ctx, chainJob(), chainV1{Left: 0})
			}
			n.nestedRan = true
			n.nestedDL = dl
			return nil
		},
	}}
}

func TestJobSubmitter_NestedInlineSharesTheOuterBudget(t *testing.T) {
	q := queue.Disabled(discardLogger())
	s := newJobSubmitter(q, nil, discardLogger())
	n := &nestJobs{s: s}
	hs := handlersOf([]queue.Provider{n})
	require.NoError(t, q.Declare(jobsOf(hs), nil))
	s.bind(hs)

	require.NoError(t, s.Submit(t.Context(), chainJob(), chainV1{Left: 1}))

	require.True(t, n.nestedRan)
	require.False(t, n.outerDL.IsZero())
	require.False(t, n.nestedDL.IsZero())
	assert.Equal(t, n.outerDL, n.nestedDL)
}

func TestJobSubmitter_NestedInlineWithSpentBudgetSkipsTheHandler(t *testing.T) {
	q := queue.Disabled(discardLogger())
	var logs bytes.Buffer
	s := newJobSubmitter(q, nil, slog.New(slog.NewTextHandler(&logs, nil)))
	n := &nestJobs{s: s}
	hs := handlersOf([]queue.Provider{n})
	require.NoError(t, q.Declare(jobsOf(hs), nil))
	s.bind(hs)
	ctx := context.WithValue(t.Context(), inlineDeadlineKey{}, time.Now().Add(-time.Second))

	err := s.Submit(ctx, chainJob(), chainV1{Left: 0})

	require.True(t, IsInlineJobError(err), "got %v", err)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.False(t, n.nestedRan)
	assert.Contains(t, logs.String(), "level=WARN")
	assert.Contains(t, logs.String(), "yasakutest.chain")
	assert.Contains(t, logs.String(), "depth=1")
}

func TestDeclareQueue_BindsTheJobSubmitter(t *testing.T) {
	cfg := newWiringConfig(t)
	mountOpensheet(cfg)
	k := newWiringKernel(t, cfg)
	k.Queue = queue.Disabled(discardLogger())
	svcs, err := buildServices(cfg, k, capabilities.Capabilities{})
	require.NoError(t, err)
	require.NotNil(t, svcs.jobs, "jobs")
	require.Nil(t, svcs.jobs.handlers.Load(), "nothing is bound before declareQueue")

	hs, _, err := declareQueue(k.Queue, svcs, &onboardingGate{}, discardLogger())
	require.NoError(t, err)

	bound := svcs.jobs.handlers.Load()
	require.NotNil(t, bound, "declareQueue must bind the submitter to the declared handlers")
	require.NotEmpty(t, hs, "opensheet.sync is declared, so the check below is not vacuous")
	require.Len(t, *bound, len(hs))
	for _, h := range hs {
		_, ok := (*bound)[h.Job]
		require.True(t, ok, "%s is declared but the submitter cannot run it inline", h.Job.Subject())
	}
}
