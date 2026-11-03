package importer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/rokumate/ronote/internal/canon"
	"github.com/rokumate/ronote/internal/extract"
	"github.com/rokumate/ronote/internal/fetch"
	"github.com/rokumate/ronote/internal/ir"
	"github.com/rokumate/ronote/internal/store"
)

// Worker runs import jobs.
type Worker struct {
	river.WorkerDefaults[Args]
	store   *store.Store
	fetcher *fetch.Fetcher
	ttl     time.Duration
	logger  *slog.Logger

	mu sync.Mutex
	// retryAfter holds, by job ID, the wait a server asked for on the
	// attempt that just failed, for NextRetry to use.
	retryAfter map[int64]time.Duration
}

// NewWorker returns a Worker that keeps fetched pages fresh for ttl.
func NewWorker(s *store.Store, f *fetch.Fetcher, ttl time.Duration, logger *slog.Logger) *Worker {
	return &Worker{store: s, fetcher: f, ttl: ttl, logger: logger, retryAfter: map[int64]time.Duration{}}
}

// Timeout bounds one attempt.
func (w *Worker) Timeout(*river.Job[Args]) time.Duration { return jobTimeout }

// NextRetry honours a Retry-After from the attempt that just failed; without
// one, River's backoff decides.
func (w *Worker) NextRetry(job *river.Job[Args]) time.Time {
	w.mu.Lock()
	wait, ok := w.retryAfter[job.ID]
	delete(w.retryAfter, job.ID)
	w.mu.Unlock()
	if !ok {
		return time.Time{}
	}
	return time.Now().Add(wait)
}

// page is a page's body and where it came from, fetched or stored.
type page struct {
	finalURL, contentType, bodySHA256, etag, lastModified string
	body                                                  []byte
	fetchedAt                                             time.Time
}

// Work runs one import: fetch (or reuse the stored copy), extract, and
// store the owner's document. The document, the import's done status and
// the job's completion commit in one transaction, so a retried job finds the
// import done and never makes a second document.
func (w *Worker) Work(ctx context.Context, job *river.Job[Args]) (err error) {
	owner, id := job.Args.OwnerID, job.Args.ImportID
	defer func() {
		// Whatever went wrong on the last attempt, the import must end.
		if err != nil && job.Attempt >= job.MaxAttempts {
			w.fail(context.WithoutCancel(ctx), owner, id, "internal_error")
		}
	}()

	imp, err := w.store.GetImport(ctx, owner, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return river.JobCancel(fmt.Errorf("import %s of user %s: %w", id, owner, err))
	case err != nil:
		return err
	case imp.Status.Terminal():
		return nil // a duplicate or retried job for an import that has finished
	}

	if doc, err := w.store.FindOwnerDocumentForURL(ctx, owner, imp.CanonicalURL); err == nil {
		return w.store.InTx(ctx, func(tx *store.Store, raw pgx.Tx) error {
			return complete(ctx, tx, raw, job, imp, doc)
		})
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}

	if done, err := w.advance(ctx, imp, store.ImportFetching); done || err != nil {
		return err
	}
	pg, err := w.load(ctx, imp)
	if err != nil {
		return w.failed(ctx, job, imp, err)
	}
	if done, err := w.advance(ctx, imp, store.ImportExtracting); done || err != nil {
		return err
	}
	doc, canonical, err := extractPage(imp, pg)
	if err != nil {
		return w.failed(ctx, job, imp, err)
	}

	return w.store.InTx(ctx, func(tx *store.Store, raw pgx.Tx) error {
		src, err := tx.UpsertSource(ctx, store.Source{
			CanonicalURL: canonical, FinalURL: pg.finalURL, ETag: pg.etag, LastModified: pg.lastModified, LastFetchedAt: pg.fetchedAt,
		})
		if err != nil {
			return err
		}
		snap, err := tx.SaveSnapshot(ctx, store.Snapshot{
			SourceID: src.ID, BodySHA256: pg.bodySHA256, Body: pg.body, ContentType: pg.contentType, FetchedAt: pg.fetchedAt,
		})
		if err != nil {
			return err
		}
		ext, err := tx.SaveExtraction(ctx, snap, doc)
		if err != nil {
			return err
		}
		docID, _, err := tx.CreateDocumentIfAbsent(ctx, store.NewDocument{
			OwnerID: owner, SourceID: src.ID, ExtractionID: ext.ID, Content: ext.Doc,
		})
		if err != nil {
			return err
		}
		return complete(ctx, tx, raw, job, imp, docID)
	})
}

// complete marks the import done with doc and the job completed, in the
// caller's transaction.
func complete(ctx context.Context, tx *store.Store, raw pgx.Tx, job *river.Job[Args], imp store.Import, doc uuid.UUID) error {
	if _, err := tx.FinishImport(ctx, imp.OwnerID, imp.ID, doc); err != nil {
		return err
	}
	_, err := river.JobCompleteTx[*riverpgxv5.Driver](ctx, raw, job)
	return err
}

// advance moves the import to status. done is true when the import has
// already finished, as when a duplicate job got there first.
func (w *Worker) advance(ctx context.Context, imp store.Import, status store.ImportStatus) (done bool, err error) {
	_, err = w.store.SetImportStatus(ctx, imp.OwnerID, imp.ID, status)
	if errors.Is(err, store.ErrNotFound) {
		return true, nil
	}
	return false, err
}

// load returns the page for an import: the stored copy while it is fresh,
// else a conditional fetch, where a 304 confirms the stored copy.
func (w *Worker) load(ctx context.Context, imp store.Import) (page, error) {
	src, err := w.store.FindSourceForURL(ctx, imp.CanonicalURL)
	known := err == nil
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return page{}, err
	}
	if known && time.Since(src.LastFetchedAt) < w.ttl {
		if pg, err := w.stored(ctx, src, src.LastFetchedAt); !errors.Is(err, store.ErrNotFound) {
			return pg, err
		}
	}

	req := fetch.Request{URL: imp.CanonicalURL}
	if known {
		req.ETag, req.LastModified = src.ETag, src.LastModified
	}
	res, err := w.fetcher.Fetch(ctx, req)
	if err != nil {
		return page{}, err
	}
	if res.NotModified {
		if err := w.store.TouchSource(ctx, src.ID, res.FetchedAt); err != nil {
			return page{}, err
		}
		if pg, err := w.stored(ctx, src, res.FetchedAt); !errors.Is(err, store.ErrNotFound) {
			return pg, err
		}
		// A 304 for a source with no snapshot: fetch it whole.
		if res, err = w.fetcher.Fetch(ctx, fetch.Request{URL: imp.CanonicalURL}); err != nil {
			return page{}, err
		}
	}
	final, err := canon.Normalize(res.FinalURL)
	if err != nil {
		return page{}, fmt.Errorf("%w: final URL: %w", fetch.ErrInvalidURL, err)
	}
	return page{
		finalURL: final, contentType: res.ContentType, bodySHA256: res.BodySHA256,
		etag: res.ETag, lastModified: res.LastModified, body: res.Body, fetchedAt: res.FetchedAt,
	}, nil
}

// stored returns the source's newest snapshot as a page fetched at at.
func (w *Worker) stored(ctx context.Context, src store.Source, at time.Time) (page, error) {
	snap, err := w.store.NewestSnapshot(ctx, src.ID)
	if err != nil {
		return page{}, err
	}
	return page{
		finalURL: src.FinalURL, contentType: snap.ContentType, bodySHA256: snap.BodySHA256,
		etag: src.ETag, lastModified: src.LastModified, body: snap.Body, fetchedAt: at,
	}, nil
}

// extractPage turns a page into IR and finds its canonical URL. The IR
// records the canonical form of the requested URL, never one user's exact
// request, since extractions are shared.
func extractPage(imp store.Import, pg page) (*ir.Doc, string, error) {
	doc, err := extract.Extract(bytes.NewReader(pg.body), ir.Source{
		URL: imp.CanonicalURL, FinalURL: pg.finalURL, FetchedAt: pg.fetchedAt,
		BodySHA256: pg.bodySHA256, ETag: pg.etag, LastModified: pg.lastModified,
	})
	if err != nil {
		return nil, "", fmt.Errorf("%w: %w", errExtract, err)
	}
	if err := ir.Validate(doc); err != nil {
		return nil, "", fmt.Errorf("%w: %w", errExtract, err)
	}
	canonical, err := canon.Resolve(pg.finalURL, doc.Source.CanonicalURL)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %w", errExtract, err)
	}
	return doc, canonical, nil
}

// failed handles an error from fetching or extracting. A permanent one fails
// the import and cancels the job. A transient one is retried, after the
// server's Retry-After if it gave one; on the last attempt it fails the
// import and River discards the job.
func (w *Worker) failed(ctx context.Context, job *river.Job[Args], imp store.Import, cause error) error {
	f := classify(cause)
	if !f.permanent && job.Attempt < job.MaxAttempts {
		if f.retryAfter > 0 {
			w.mu.Lock()
			w.retryAfter[job.ID] = min(f.retryAfter, maxRetryAfter)
			w.mu.Unlock()
		}
		return cause
	}
	w.fail(ctx, imp.OwnerID, imp.ID, f.code)
	if f.permanent {
		return river.JobCancel(cause)
	}
	return cause
}

// fail marks an unfinished import failed with code; one that has already
// finished keeps its result.
func (w *Worker) fail(ctx context.Context, owner, id uuid.UUID, code string) {
	if _, err := w.store.FailImport(ctx, owner, id, code); err != nil && !errors.Is(err, store.ErrNotFound) {
		w.logger.ErrorContext(ctx, "could not mark import failed", "import_id", id.String(), "code", code, "error", err)
	}
}
