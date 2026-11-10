# 0009: The jobs port

Accepted, 2026-11-10

## Context

Imports run as River jobs (ADR 0005), and River reaches far past the queue:

- the importer's types are River's (`river.Job`, `river.Client`, `WorkerDefaults`);
- the import's `done` status, its document and River's job completion commit in one transaction (`JobCompleteTx`);
- a 429's `Retry-After` travels to River's `NextRetry` through a per-job map in the worker;
- both commands build River clients.

Moving off River, to a broker or to a second Postgres-free deployment, would mean rewriting the pipeline. Unit tests can't run an import's retry rules without Postgres.

## Decision

- **A port, `internal/jobs`, between the code that has work done and whatever runs it:**
  - `Job{Kind, Key, Payload}`: Payload is JSON; an empty Key means none.
  - `Delivery{Job, Attempt}`: Attempt counts from 1.
  - `Handler func(ctx, Delivery) error`.
  - `Enqueuer.EnqueueTx(ctx, tx, job)`, where tx is the caller's `pgx.Tx`.
  - `Runner.Handle(kind, handler, Options)` and `Runner.Run(ctx)`.
  - `Options{MaxAttempts, Concurrency, Timeout}`, per kind.
  - A handler's error says what happens next: `jobs.Permanent(err)` or `jobs.RetryAfter(err, d)`, or a plain error.
- **The contract every adapter keeps:**
  - delivery is at least once, so handlers are idempotent;
  - a job enqueued in a transaction exists exactly when that transaction commits;
  - a plain error retries with the adapter's backoff, up to the kind's `MaxAttempts`;
  - `Permanent` stops retries at once;
  - `RetryAfter` makes the next attempt wait at least that long;
  - at most `Concurrency` handlers of a kind run at once, and each attempt's context ends at `Timeout`;
  - jobs with the same non-empty Key run once;
  - `Run` returns once its context ends and the handlers running then have finished, cancelling them after a grace period.
- **Completion is the adapter's, after the handler returns:** no `JobCompleteTx`. A job redelivered after its work committed, say because the process died first, finds the import done and does nothing.
- **Adapters:**
  - **`riverjobs`, the default:** River on Postgres. It is the only package that may import `github.com/riverqueue`, enforced by depguard in `.golangci.yml`.
    - Each kind is its own River queue, with `MaxWorkers` set to its concurrency.
    - River keeps a job's attempt limit on its row, set when the job is inserted. So a process that only enqueues, like the API, declares each kind's options too (`riverjobs.Options.Kinds`), and enqueuing an undeclared kind fails.
    - A Key is a `river:"unique"` field of the job's args, unique across River's live and completed states, until River deletes completed jobs (24 h by default).
    - `Permanent` becomes `JobCancel`.
    - `RetryAfter` goes through River's retry path, the adapter's `NextRetry`, never `JobSnooze`, so migration 00006's clock trigger also moves those retries onto the database's clock.
    - `Run` calls `Stop`, then `StopAndCancel` once its grace period is over.
    - Migration 00006's trigger belongs to this adapter: the file stays with goose, which owns all schema, and its test lives in `riverjobs`.
  - **`memjobs`:** in memory, for unit tests. Its clock is injectable, and its transactions only commit or roll back.
- **One conformance suite, `jobstest`**, runs against both adapters: redelivery, Permanent, RetryAfter, rollback, Key, concurrency, shutdown, the attempt limit and the timeout.
- **A broker other than Postgres** (Redis, RabbitMQ, NATS) keeps enqueueing in the caller's transaction through an outbox. This is specified, not built:
  - an outbox Enqueuer inserts into `jobs_outbox(id, kind, key, payload, created_at, published_at)` in the caller's transaction;
  - a relay claims unpublished rows with `FOR UPDATE SKIP LOCKED`, woken by LISTEN or a poll. It publishes each to the broker with the row's id as the message id, so the broker can drop a duplicate (a Key becomes the broker's de-duplication id), then sets `published_at`. A relay that dies between publishing and marking publishes again: at least once, as the contract says;
  - the consumer adapter acknowledges a message after its handler returns;
  - a plain error becomes the broker's redelivery with backoff, `RetryAfter` a delayed message, `Permanent` an acknowledgement and a dead letter;
  - concurrency becomes the consumer's prefetch, and the attempt count the broker's delivery count.
- **`internal/events` is the other Postgres coupling.** Import status changes reach the API through a trigger's `pg_notify` and one LISTEN connection per process (ADR 0005). Swapping it would take:
  - a pub/sub channel on the new broker;
  - status changes published after their transaction commits, through the same outbox rather than the trigger;
  - the reread after a reconnect, as today.

  It stays as it is until a broker arrives.

## Consequences

- Importer code knows `jobs`, never River. Its unit tests run retry rules on `memjobs` without Postgres; its integration tests run on real River.
- A job's work and its completion no longer commit together. The window between them is a redelivery, which the handler's idempotency covers.
- Jobs queued in River's old args shape (`{import_id, owner_id}` at the top level) don't read in the new one (`{key, payload}`). Nothing is deployed, so only local databases hold any.
- A new adapter passes `jobstest` before it is used.
