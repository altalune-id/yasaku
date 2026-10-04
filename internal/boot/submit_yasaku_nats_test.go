package boot

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/platform/queue"
)

const (
	natsWait       = 5 * time.Second
	natsTick       = 20 * time.Millisecond
	echoDLQSubject = "dlq.jobs.yasakutest.echo.v1"
	echoDurable    = "yasakutest_echo_v1"
)

type reports struct {
	mu   sync.Mutex
	msgs []string
}

func (r *reports) unexpected(_ context.Context, msg string, _ error, _ ...any) *apperror.AppError {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, msg)
	return nil
}

func (r *reports) Messages() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.msgs...)
}

type natsEnv struct {
	js        jetstream.JetStream
	client    *queue.Client
	submitter *jobSubmitter
	handlers  []queue.Handler
	reports   *reports
}

func newNATSEnv(t *testing.T, jobs *echoJobs) natsEnv {
	t.Helper()
	ns := startNATSServer(t)
	rep := &reports{}
	q, err := queue.Connect(t.Context(), queue.Options{URL: ns.ClientURL(), ConnectTimeout: natsWait, Log: discardLogger(), Unexpected: rep.unexpected})
	require.NoError(t, err)
	t.Cleanup(func() { _ = q.Close() })
	nc, err := nats.Connect(ns.ClientURL())
	require.NoError(t, err)
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	require.NoError(t, err)
	hs := handlersOf([]queue.Provider{jobs})
	require.NoError(t, q.Declare(jobsOf(hs), nil))
	s := newJobSubmitter(q, nil, discardLogger())
	s.bind(hs)
	return natsEnv{js: js, client: q, submitter: s, handlers: hs, reports: rep}
}

func (e natsEnv) consumer(t *testing.T) *queue.Consumer {
	t.Helper()
	cons, err := queue.NewConsumer(t.Context(), e.client, e.handlers)
	require.NoError(t, err)
	return cons
}

func runConsumer(t *testing.T, cons *queue.Consumer) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- cons.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			assert.NoError(t, err)
		case <-time.After(10 * time.Second):
			t.Error("queue consumer did not stop")
		}
	})
}

func (e natsEnv) stream(t *testing.T, name string) jetstream.Stream {
	t.Helper()
	s, err := e.js.Stream(context.Background(), name)
	require.NoError(t, err)
	return s
}

func (e natsEnv) msgs(t *testing.T, name string) uint64 {
	t.Helper()
	info, err := e.stream(t, name).Info(context.Background())
	require.NoError(t, err)
	return info.State.Msgs
}

func waitRun(t *testing.T, jobs *echoJobs, timeout time.Duration) echoRun {
	t.Helper()
	select {
	case r := <-jobs.ran:
		return r
	case <-time.After(timeout):
		t.Fatal("the handler did not run")
		return echoRun{}
	}
}

func TestJobSubmitter_EnabledPublishesAndDoesNotRunInline(t *testing.T) {
	t.Parallel()
	jobs := newEchoJobs()
	env := newNATSEnv(t, jobs)

	require.NoError(t, env.submitter.Submit(t.Context(), echoJob(), echoV1{Text: "queued"}))

	require.Equal(t, uint64(1), env.msgs(t, "WORK"))
	require.Empty(t, jobs.Runs(), "an enabled queue never runs the handler inline")
}

func TestJobSubmitter_NATSSubmitConsumeAck(t *testing.T) {
	t.Parallel()
	jobs := newEchoJobs()
	env := newNATSEnv(t, jobs)
	runConsumer(t, env.consumer(t))
	ctx, tc := scopedCtx(t.Context())

	require.NoError(t, env.submitter.Submit(ctx, echoJob(), echoV1{Text: "hi"}))

	r := waitRun(t, jobs, natsWait)
	assert.Equal(t, echoV1{Text: "hi"}, r.Payload)
	assert.Equal(t, tc, r.Tenant, "the consumer binds the same tenant the inline path does")
	assert.Equal(t, 1, r.Msg.NumDelivered)
	require.Eventually(t, func() bool { return env.msgs(t, "WORK") == 0 }, natsWait, natsTick, "an acked job leaves WORK")
	assert.Equal(t, uint64(0), env.msgs(t, "DLQ"))
}

func TestJobSubmitter_NATSPermanentGoesToDLQWithTheInlineReason(t *testing.T) {
	t.Parallel()
	jobs := newEchoJobs()
	jobs.fail = func(int) error { return queue.Permanent(errors.New("bad config")) }
	env := newNATSEnv(t, jobs)
	runConsumer(t, env.consumer(t))

	require.NoError(t, env.submitter.Submit(t.Context(), echoJob(), echoV1{}))

	waitRun(t, jobs, natsWait)
	require.Eventually(t, func() bool { return env.msgs(t, "DLQ") == 1 }, natsWait, natsTick)
	dl, err := env.stream(t, "DLQ").GetLastMsgForSubject(context.Background(), echoDLQSubject)
	require.NoError(t, err)
	assert.Equal(t, inlineReasonPermanent, dl.Header.Get("Yasaku-Dlq-Reason"), "inline and DLQ reasons must match")
	assert.Len(t, jobs.Runs(), 1, "a permanent error is not retried")
	require.Eventually(t, func() bool { return slices.Contains(env.reports.Messages(), "queue: dead-lettered") }, natsWait, natsTick)
}

func TestJobSubmitter_NATSExhaustedReasonMatchesInline(t *testing.T) {
	t.Parallel()
	jobs := newEchoJobs()
	env := newNATSEnv(t, jobs)
	cons := env.consumer(t)
	require.NoError(t, env.submitter.Submit(t.Context(), echoJob(), echoV1{}))

	raw, err := env.js.Consumer(t.Context(), "WORK", echoDurable)
	require.NoError(t, err)
	for range queue.MaxAttempts {
		batch, fErr := raw.Fetch(1, jetstream.FetchMaxWait(natsWait))
		require.NoError(t, fErr)
		got := 0
		for m := range batch.Messages() {
			require.NoError(t, m.Nak())
			got++
		}
		require.NoError(t, batch.Error())
		require.Equal(t, 1, got)
	}
	runConsumer(t, cons)

	require.Eventually(t, func() bool { return env.msgs(t, "DLQ") == 1 }, natsWait, natsTick)
	dl, err := env.stream(t, "DLQ").GetLastMsgForSubject(context.Background(), echoDLQSubject)
	require.NoError(t, err)
	assert.Equal(t, inlineReasonExhausted, dl.Header.Get("Yasaku-Dlq-Reason"), "inline and DLQ reasons must match")
	assert.Empty(t, jobs.Runs(), "a delivery past the cap is dead-lettered without running the handler")
}
