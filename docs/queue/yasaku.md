# yasaku queue jobs

The template contract is [`queue`](README.md) and the recipe is
[`howto/queue-consumer.md`](../howto/queue-consumer.md). This page is what yasaku adds on top. Running
NATS: [`deployment/yasaku.md`](../deployment/yasaku.md).

## Submit through `svcs.jobs`, never `Kernel.Queue`

NOTE (template gap T32): with `queue.enabled=false` the template's `Submit` returns nil and drops the
job. Yasaku services never receive `Kernel.Queue`. Boot hands them `svcs.jobs`, a `*jobSubmitter`
(`internal/boot/submit_yasaku.go`), through the same one-method port the template uses:

```go
type Queue interface {
	Submit(ctx context.Context, j queue.Job, data any) error
}
```

| `queue.enabled` | `Submit` does                                                                 |
| --------------- | ----------------------------------------------------------------------------- |
| `true`          | the template's `Client.Submit`: publish to `WORK`, a consumer runs it         |
| `false`         | runs the job's own `queue.Handler` inline, in the caller's goroutine, at once |

`declareQueue` binds the submitter to the same handlers it declares and consumes. A guard test
(`submit_yasaku_guard_test.go`) fails when any value's `.Queue` (`k.Queue`, `kernel.Queue`,
`svcs.Kernel.Queue`, …) reaches a boot site other than the template's todo wiring and the submitter
itself; `cfg.Queue` and a package's `Queue` type are not values.

## The inline run

The handler sees what a consumer would give it, so a handler that passes inline also passes on NATS:

- **Context:** the caller's trace and tenant (org, project, user) only. No request id, no principal,
  no unit-of-work transaction, and the caller's cancellation does not reach it.
- **Message:** `json.Marshal(data)` as `Data`, a UUIDv7 `ID`, `NumDelivered` 1.
- **One delivery, no retry.** A panic is an error.
- **Budget: 20s in total for the whole inline chain** (`inlineBudget`), shared by the handler,
  `OnDeadLetter` and any nested job. It sits under the consumer's 25s `HandlerTimeout`. The HTTP
  server's `WriteTimeout` is 30s and the inline run is part of the request, so request work before
  the `Submit` plus the job must fit in 30s. Keep inline handlers well under the budget.
- **`OnDeadLetter`:** it shares the same budget, so after a timeout it may get little or no time.
  Keep it to cheap, idempotent work.
- **Shutdown:** a job still running inline when the process stops is lost without a report. Only
  the queue survives a restart.
- **Depth:** a handler may submit one more job from its own ctx (for example `recurring.post` then
  `opensheet.sync`), so inline depth 2 is allowed. A third level returns `*boot.InlineDepthError`.

| Handler returns             | `Submit` returns                                                     |
| --------------------------- | -------------------------------------------------------------------- |
| nil                         | nil                                                                  |
| `queue.Permanent(err)`      | `*boot.InlineJobError`, reason `permanent`; `queue.IsPermanentError` |
| any other error, or a panic | `*boot.InlineJobError`, reason `exhausted`                           |
| an error, `SuppressReport`  | nil, after a Warn log                                                |

On an error `OnDeadLetter` runs first, with the same reason. The reason strings are the consumer's;
a test pins them against a real DLQ `Yasaku-Dlq-Reason` header.

If the chain budget is already spent, the nested job is skipped: neither the handler nor
`OnDeadLetter` runs, `queue: inline job skipped, chain budget spent` is logged at Warn, and `Submit`
returns `*boot.InlineJobError` (reason `exhausted`, wraps `context.DeadlineExceeded`), even with
`SuppressReport`.

NOTE (parity gap): inline, a **transient** error calls `OnDeadLetter` with reason `exhausted` after
one attempt. On NATS the consumer would retry it 4 more times first. A handler's `OnDeadLetter` must
not assume a transient failure was retried.

The caller rule does not change: report the error with
`s.unexpected(ctx, "<module>.<Method>: submit", err, …)` and still succeed.

## Refused in both modes

| Error                           | When                                                       |
| ------------------------------- | ---------------------------------------------------------- |
| `*boot.SubmitInUnitOfWorkError` | `Submit` inside a unit of work (`db.CurrentTx` is set)     |
| `*queue.UndeclaredJobError`     | the job has no handler in the published consumers          |
| `*queue.PublishError`           | the payload does not encode, or NATS did not ack within 5s |
| `*boot.InlineDepthError`        | queue disabled only: a third level of inline jobs          |

NOTE (false negative): the unit-of-work check reads the ctx passed to `Submit`. A closure inside
`s.uow(ctx, func(txCtx context.Context) error { … })` that calls `Submit` with the captured outer
`ctx` instead of `txCtx` is not caught, and runs before the commit. Submit after the `uow` call
returns, never inside its closure.

## Registered jobs

| Job                 | Handler                              | Payload                                               | Failure handling                                                    |
| ------------------- | ------------------------------------ | ----------------------------------------------------- | ------------------------------------------------------------------- |
| `opensheet.sync` v1 | `internal/opensheetsync/consumer.go` | `{project_id, refs: [{entity, id}]}`, at most 50 refs | `SuppressReport`, no `OnDeadLetter`; refusals are `queue.Permanent` |

It is declared only when the Opensheet module is mounted ([`opensheet/yasaku.md`](../opensheet/yasaku.md)).
Its reconciler, the scheduler job `opensheet-reconcile`, re-submits rows a lost job left dirty. With
the queue on, a full job submits one follow-up job from its own ctx.

## Registering a yasaku job

1. Job, payload and handler in the module's `consumer.go`, exactly as the template recipe says.
2. The service declares the `Queue` port above and takes it in `NewService`.
3. Boot: pass `jobs` (the local in `buildServices`) to `NewService`. Add the module name to
   `yasakuConsumerDomains()` and its `NewConsumer(...)` to `yasakuConsumerProviders(s, log)` in
   `internal/boot/consumers_yasaku.go`, in the same order. The template's `consumers.go` appends both
   lists; do not edit it.
4. Tests: service tests on `fakes.Queue`; the handler through `ConsumerHandlers()`; the full path on
   the embedded server. `internal/boot/submit_yasaku_nats_test.go` and its `yasakutest.echo` job are
   the pattern.

A scheduler job registers the same way through `yasakuSchedulerDomains()` and
`yasakuSchedulerProviders()` in `internal/boot/schedulers_yasaku.go`; the template's `schedulers.go`
appends both. A module's console page goes in `yasakuConsoleHandlers` (`internal/boot/surfaces_yasaku.go`).
An optional module always builds its providers and declares no job while unmounted, so the domain
lists stay fixed. Add its stubs to `schedulers_yasaku_test.go` / `consumers_yasaku_test.go`; no
template test changes. `internal/opensheetsync` is the reference.
