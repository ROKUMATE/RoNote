# 0005: Import pipeline, caching and idempotency

Accepted, 2026-11-04. Amended 2026-11-10: jobs run through the jobs port, and their completion leaves the final transaction (ADR 0009).

## Context

From P08 on, a user's import is stored: the API takes a URL, a worker fetches and extracts the page, and the user gets their own document. Many users import the same pages, sites must not be fetched more than needed, jobs can run twice, and one user must never see another's data.

## Decision

- **Two processes.** The API validates the URL, then canonicalizes it (`internal/canon`):
  - lowercase scheme and host, default port dropped, fragment dropped;
  - `utm_*`, `gclid` and `fbclid` dropped; IP-literal hosts refused.

  If the user already has a document for that URL, the API answers 200 with it. Otherwise it creates the import (`queued`) and its job in one transaction (202), through the jobs port (ADR 0009). The worker (`cmd/worker`, kind `import` on River, 4 at a time, 60s per attempt) runs the job.
- **Shared cache, private documents.**
  - The cache is shared by all users: a source per canonical URL, a snapshot per distinct body (gzip in Postgres for now), and an extraction per snapshot and extractor version.
  - A document is the user's own IR copy, one per owner and source (`documents_owner_source_key`).
  - Extractions record the canonical URL, never one user's exact request.
- **Freshness.**
  - A source fetched or confirmed within `RONOTE_SOURCE_TTL` (24h) is not fetched again.
  - After that, a conditional GET; a 304 confirms the stored snapshot and restarts the clock.
  - The extractor always runs, since it is milliseconds and deterministic. Its result is stored only when that snapshot has no extraction by that extractor version; otherwise the stored one is reused, so detection always follows the current code.
- **Canonical URLs.** A page's `<link rel=canonical>` is trusted only on the page's own host. Otherwise any page could claim another site's URL and replace that site's cached copy for everyone.
- **Idempotency.**
  - The worker writes source, snapshot, extraction, document and the `done` status in one final transaction. Every insert there reuses existing rows on its unique key. The job completes after it (ADR 0009, which replaced River's `JobCompleteTx` in that transaction).
  - A retried or duplicate job finds the import done, or the document already there; it never makes a second one.
- **Errors.**
  - **Permanent** (`jobs.Permanent`, River's `JobCancel`, on the first attempt): invalid URL, blocked address, robots.txt disallow, too large, unsupported type, too many redirects, 404/410, other 4xx, extraction failure.
  - **Transient** (River backoff, 3 attempts in all): timeout, 5xx, 429, network errors, robots.txt answering 5xx (RFC 9309: nothing is fetched until it can be read).
  - A `Retry-After` on a 429 or 503 sets the next attempt's time, capped at 10 minutes.
  - The last failed attempt marks the import failed with a stable `error_code`.
- **Progress.** A trigger on `imports` sends `pg_notify('import_status', …)` for every new import and status change, inside the transaction that makes it. Each API process holds one LISTEN connection and fans events out in memory to `GET /api/v1/imports/{id}/events` (SSE). A stream:
  - subscribes before reading the import;
  - sends the current status, then only forward moves, and closes at done or failed;
  - sends a heartbeat every 15s;
  - rereads the import after the listener reconnects.
- **Owner scoping.**
  - Every query on documents or imports takes `owner_id` (a test fails on any that does not).
  - An import can only reference a document of the same owner (composite foreign key).
  - Another user's rows answer 404.
- **Rate limit.** The fetcher's 1 request per second per host holds per worker process. Several worker processes multiply it.
- **Schema.** Goose owns all schema, River's tables included (`make river-migration`). A test fails when the pinned River knows a migration we have not added.
- **One clock for jobs (P12).** River writes a job's times from the app's clock but claims jobs by the database's `now()`:
  - its pgx driver fills a new job's `created_at` and `scheduled_at` with `time.Now()`;
  - a retry's time is `time.Now()` plus a backoff, from River's policy or the River adapter's `NextRetry` (a handler's `jobs.RetryAfter`);
  - the claim takes `available` jobs whose `scheduled_at` is at or before `now()`.

  When the podman VM's clock lagged the Mac's by 19 minutes after sleep, every new job was 19 minutes in the database's future, and imports stayed `queued`. A database clock running ahead let River's scheduler hand retries to the claim early.

  Migration 00006's trigger on `river_job`, amended by 00008, moves those times onto the database's clock:
  - **New jobs:** shifted by the database's `now()` minus the app's `created_at`.
  - **Retries:** the River adapter sends a delay as that far past the year 3000, a time River accepts as in the future, and migration 00008's trigger makes it `now()` plus the delay. Migration 00006 had shifted the app's time by `attempted_at` minus the failed attempt's error time; the two differ by the wait between the claim and the executor's start, so a retry came that much early (a flaky `RetryAfter` conformance case under load).
  - **State:** both go straight to `available`, which River's completer already treats as a retry.

  So the claim is the only comparison that decides when a job runs, and it uses the database's clock. `TestJobsRunWhateverTheAppClock` fakes a half-hour offset either way.

  Left on the app's clock:
  - River's stuck-job rescue. A skew over an hour can rescue a running job early; imports are idempotent.
  - Snoozed jobs; RoNote never snoozes.

## Consequences

- Imports of popular pages cost one fetch per TTL period, whoever asks.
- A user's document keeps the IR it was made from; re-importing returns it unchanged. Syncing to newer snapshots comes with editing (P10+).
- Raw HTML lives in Postgres (5 MiB cap per page) until file storage arrives with PDFs (P13).
- Running more than one worker process needs a shared rate limit first.
- Real sign-in (P16) replaces the dev identity; until then staging and prod answer 401 on `/api/v1`.
