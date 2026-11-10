package importer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/rokumate/ronote/internal/canon"
	"github.com/rokumate/ronote/internal/extract"
	"github.com/rokumate/ronote/internal/fetch"
	"github.com/rokumate/ronote/internal/ir"
	"github.com/rokumate/ronote/internal/jobs"
	"github.com/rokumate/ronote/internal/store"
)

// Worker runs import jobs.
type Worker struct {
	store   *store.Store
	fetcher *fetch.Fetcher
	ttl     time.Duration
	logger  *slog.Logger
}

// NewWorker returns a Worker that keeps fetched pages fresh for ttl.
func NewWorker(s *store.Store, f *fetch.Fetcher, ttl time.Duration, logger *slog.Logger) *Worker {
	return &Worker{store: s, fetcher: f, ttl: ttl, logger: logger}
}

// page is a page's body and where it came from, fetched or stored.
type page struct {
	finalURL, contentType, bodySHA256, etag, lastModified string
	body                                                  []byte
	fetchedAt                                             time.Time
}

// Handle runs one attempt at an import job: fetch (or reuse the stored
// copy), extract, and store the owner's document. The document and the
// import's done status commit in one transaction; the job completes after
// it, so a job delivered again finds the import done and does nothing.
func (w *Worker) Handle(ctx context.Context, d jobs.Delivery) (err error) {
	var args Args
	if err := json.Unmarshal(d.Job.Payload, &args); err != nil {
		return jobs.Permanent(fmt.Errorf("import job payload: %w", err))
	}
	owner, id := args.OwnerID, args.ImportID
	defer func() {
		// Whatever went wrong on the last attempt, the import must end.
		if err != nil && d.Attempt >= maxAttempts {
			w.fail(context.WithoutCancel(ctx), owner, id, codeInternal)
		}
	}()

	imp, err := w.store.GetImport(ctx, owner, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return jobs.Permanent(fmt.Errorf("import %s of user %s: %w", id, owner, err))
	case err != nil:
		return err
	case imp.Status.Terminal():
		return nil // a duplicate or retried job for an import that has finished
	}

	if doc, err := w.store.FindOwnerDocumentForURL(ctx, owner, imp.CanonicalURL); err == nil {
		return w.store.InTx(ctx, func(tx *store.Store, _ pgx.Tx) error {
			return complete(ctx, tx, imp, doc)
		})
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}

	if done, err := w.advance(ctx, imp, store.ImportFetching); done || err != nil {
		return err
	}
	pg, err := w.load(ctx, imp)
	if err != nil {
		return w.failed(ctx, d.Attempt, imp, err)
	}
	if done, err := w.advance(ctx, imp, store.ImportExtracting); done || err != nil {
		return err
	}
	doc, canonical, err := extractPage(imp, pg)
	if err != nil {
		return w.failed(ctx, d.Attempt, imp, err)
	}

	return w.store.InTx(ctx, func(tx *store.Store, _ pgx.Tx) error {
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
		return complete(ctx, tx, imp, docID)
	})
}

// complete marks the import done with doc, in the caller's transaction.
func complete(ctx context.Context, tx *store.Store, imp store.Import, doc uuid.UUID) error {
	_, err := tx.FinishImport(ctx, imp.OwnerID, imp.ID, doc)
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

// failed handles an error from fetching or extracting: it fails the import
// when outcome says so, and returns what the job does next.
func (w *Worker) failed(ctx context.Context, attempt int, imp store.Import, cause error) error {
	code, err := outcome(cause, attempt)
	if code != "" {
		w.fail(ctx, imp.OwnerID, imp.ID, code)
	}
	return err
}

// outcome says what an error from fetching or extracting means on attempt:
// the code to fail the import with now, if any, and the error for the jobs
// port. A permanent error fails the import and stops the job. A transient
// one is retried, after the server's Retry-After if it gave one, capped at
// maxRetryAfter; on the last attempt it fails the import.
func outcome(cause error, attempt int) (code string, err error) {
	f := classify(cause)
	switch {
	case f.permanent:
		return f.code, jobs.Permanent(cause)
	case attempt >= maxAttempts:
		return f.code, cause
	case f.retryAfter > 0:
		return "", jobs.RetryAfter(cause, min(f.retryAfter, maxRetryAfter))
	default:
		return "", cause
	}
}

// fail marks an unfinished import failed with code; one that has already
// finished keeps its result.
func (w *Worker) fail(ctx context.Context, owner, id uuid.UUID, code string) {
	if _, err := w.store.FailImport(ctx, owner, id, code); err != nil && !errors.Is(err, store.ErrNotFound) {
		w.logger.ErrorContext(ctx, "could not mark import failed", "import_id", id.String(), "code", code, "error", err)
	}
}
