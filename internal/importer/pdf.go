package importer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/rokumate/ronote/internal/blob"
	"github.com/rokumate/ronote/internal/docling"
	"github.com/rokumate/ronote/internal/jobs"
	"github.com/rokumate/ronote/internal/store"
)

// MaxPDFBytes is the largest PDF RoNote takes (docs/adr/0013-pdf-import.md).
const MaxPDFBytes = 25 << 20

// Errors for uploads refused before anything is stored.
var (
	ErrNotPDF      = errors.New("the file doesn't start with %PDF-")
	ErrPDFTooLarge = errors.New("the PDF is larger than 25 MB")
)

// PDFKey is where owner's PDF with hex SHA-256 sha is kept. Keys under
// "pdf/<owner>/" are owner's alone, so deleting an account deletes them.
func PDFKey(owner uuid.UUID, sha string) string {
	return ownerPrefix(owner) + sha
}

func ownerPrefix(owner uuid.UUID) string {
	return "pdf/" + owner.String() + "/"
}

// cleanFilename keeps a filename people recognise and nothing that could
// mislead: the last path element, without control characters, at most 255
// bytes, and "document.pdf" when nothing is left.
func cleanFilename(name string) string {
	name = path.Base(strings.ReplaceAll(name, `\`, "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == utf8.RuneError {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	for len(name) > 255 {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	if name == "" || name == "." || name == "/" {
		return "document.pdf"
	}
	return name
}

// RequestPDF starts an import of the PDF in file, size bytes, for owner, or
// returns owner's existing document for it. The PDF goes to blob storage
// first; the import and its job are then created in one transaction, over
// the same quota as pages.
func (s *Service) RequestPDF(ctx context.Context, owner uuid.UUID, filename string, file io.ReadSeeker, size int64) (Requested, error) {
	if size > MaxPDFBytes {
		return Requested{}, ErrPDFTooLarge
	}
	magic := make([]byte, 5)
	if _, err := io.ReadFull(file, magic); err != nil || string(magic) != "%PDF-" {
		return Requested{}, ErrNotPDF
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return Requested{}, err
	}
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return Requested{}, err
	}
	sha := hex.EncodeToString(h.Sum(nil))

	doc, err := s.store.FindOwnerPDFDocument(ctx, owner, sha)
	switch {
	case err == nil:
		imp, err := s.store.LatestImportForDocument(ctx, owner, doc)
		if err == nil {
			return Requested{Import: imp, Existing: true}, nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return Requested{}, err
		}
	case !errors.Is(err, store.ErrNotFound):
		return Requested{}, err
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return Requested{}, err
	}
	// The blob lands before the import that names it; one whose import
	// never commits is swept later.
	if err := s.blobs.Put(ctx, PDFKey(owner, sha), file, size); err != nil {
		return Requested{}, err
	}
	var imp store.Import
	err = s.store.InTx(ctx, func(tx *store.Store, raw pgx.Tx) error {
		if err := s.checkQuota(ctx, tx, owner); err != nil {
			return err
		}
		var err error
		if imp, err = tx.CreatePDFImport(ctx, owner, cleanFilename(filename), sha); err != nil {
			return err
		}
		j, err := pdfJob(Args{ImportID: imp.ID, OwnerID: owner})
		if err != nil {
			return err
		}
		return s.queue.EnqueueTx(ctx, raw, j)
	})
	if err != nil {
		return Requested{}, err
	}
	return Requested{Import: imp}, nil
}

// DeleteDocument deletes owner's document id (store.RemoveDocument) and, in
// the same transaction, enqueues the deletion of its PDF's blob. It returns
// the conversations deleted with it.
func (s *Service) DeleteDocument(ctx context.Context, owner, id uuid.UUID) ([]uuid.UUID, error) {
	var deleted store.Deleted
	err := s.store.InTx(ctx, func(tx *store.Store, raw pgx.Tx) error {
		var err error
		if deleted, err = tx.RemoveDocument(ctx, owner, id); err != nil {
			return err
		}
		if len(deleted.BlobKeys) == 0 {
			return nil
		}
		return s.enqueueBlobDeletion(ctx, raw, BlobArgs{Keys: deleted.BlobKeys})
	})
	return deleted.Conversations, err
}

// DeleteUser deletes user's account and everything in it, and, in the same
// transaction, enqueues the deletion of their blobs.
func (s *Service) DeleteUser(ctx context.Context, user uuid.UUID) error {
	return s.store.InTx(ctx, func(tx *store.Store, raw pgx.Tx) error {
		if err := tx.DeleteUser(ctx, user); err != nil {
			return err
		}
		return s.enqueueBlobDeletion(ctx, raw, BlobArgs{Prefix: ownerPrefix(user)})
	})
}

func (s *Service) enqueueBlobDeletion(ctx context.Context, tx pgx.Tx, args BlobArgs) error {
	payload, err := json.Marshal(args)
	if err != nil {
		return err
	}
	return s.queue.EnqueueTx(ctx, tx, jobs.Job{Kind: DeleteBlobsKind, Payload: payload})
}

// PDFWorker runs PDF import jobs: inspect the PDF, convert it with docling,
// read its code, prose and pictures from the text layer, map it to IR, and
// keep it as the owner's document.
type PDFWorker struct {
	store  *store.Store
	blobs  blob.Store
	client *docling.Client
	// docling is the docling-serve version extractions record.
	docling string
	logger  *slog.Logger
}

// NewPDFWorker returns a PDFWorker that converts with client, recording
// doclingVersion in each extraction.
func NewPDFWorker(s *store.Store, blobs blob.Store, client *docling.Client, doclingVersion string, logger *slog.Logger) *PDFWorker {
	return &PDFWorker{store: s, blobs: blobs, client: client, docling: doclingVersion, logger: logger}
}

// Handle runs one attempt at a PDF import job. Like a page's, the document
// and the import's done status commit in one transaction, so a job delivered
// again finds the import done and does nothing.
func (w *PDFWorker) Handle(ctx context.Context, d jobs.Delivery) (err error) {
	var args Args
	if err := json.Unmarshal(d.Job.Payload, &args); err != nil {
		return jobs.Permanent(fmt.Errorf("PDF import job payload: %w", err))
	}
	owner, id := args.OwnerID, args.ImportID
	defer func() {
		if err != nil && d.Attempt >= maxAttempts {
			fail(context.WithoutCancel(ctx), w.store, w.logger, owner, id, codeInternal)
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
	if doc, err := w.store.FindOwnerPDFDocument(ctx, owner, imp.PDFSHA256); err == nil {
		return w.store.InTx(ctx, func(tx *store.Store, _ pgx.Tx) error { return complete(ctx, tx, imp, doc) })
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}

	if done, err := advance(ctx, w.store, imp, store.ImportConverting); done || err != nil {
		return err
	}
	key := PDFKey(owner, imp.PDFSHA256)
	pdf, err := w.read(ctx, key)
	if err != nil {
		return err
	}
	converted, err := w.convert(ctx, imp, pdf)
	if err != nil {
		code, err := pdfOutcome(err, d.Attempt)
		if code != "" {
			fail(ctx, w.store, w.logger, owner, id, code)
		}
		return err
	}
	if done, err := advance(ctx, w.store, imp, store.ImportExtracting); done || err != nil {
		return err
	}
	doc, err := docling.Map(converted.doc, docling.PDF{
		Filename: imp.Filename, SHA256: imp.PDFSHA256, ConvertedAt: time.Now(), Docling: w.docling,
	}, converted.layer)
	if err != nil {
		code, err := pdfOutcome(err, d.Attempt)
		fail(ctx, w.store, w.logger, owner, id, code)
		return err
	}

	return w.store.InTx(ctx, func(tx *store.Store, _ pgx.Tx) error {
		src, snap, err := tx.SavePDFSource(ctx, store.PDFSource{OwnerID: owner, SHA256: imp.PDFSHA256, BlobKey: key, ConvertedAt: doc.Source.FetchedAt})
		if err != nil {
			return err
		}
		ext, err := tx.SaveExtraction(ctx, snap, doc)
		if err != nil {
			return err
		}
		docID, _, err := tx.CreateDocumentIfAbsent(ctx, store.NewDocument{OwnerID: owner, SourceID: src, ExtractionID: ext.ID, Content: ext.Doc})
		if err != nil {
			return err
		}
		return complete(ctx, tx, imp, docID)
	})
}

// read loads the PDF from blob storage. A missing blob is an import that
// can't go on: its upload never landed or was swept.
func (w *PDFWorker) read(ctx context.Context, key string) ([]byte, error) {
	obj, err := w.blobs.Get(ctx, key)
	if errors.Is(err, blob.ErrNotFound) {
		return nil, jobs.Permanent(err)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = obj.Body.Close() }()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, io.LimitReader(obj.Body, MaxPDFBytes+1)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type converted struct {
	doc   *docling.Document
	layer map[string][]docling.Line
}

// convert inspects the PDF, keeping its page count on the import, even one
// refused for it, has docling convert it, and reads its code, prose and
// pictures from the text layer.
func (w *PDFWorker) convert(ctx context.Context, imp store.Import, pdf []byte) (converted, error) {
	inspection, err := w.client.Inspect(ctx, pdf)
	if inspection.Pages > 0 {
		if err := w.store.SetImportPDFPages(ctx, imp.OwnerID, imp.ID, inspection.Pages); err != nil {
			return converted{}, err
		}
	}
	if err != nil {
		return converted{}, err
	}
	doc, err := w.client.Convert(ctx, pdf, imp.Filename)
	if err != nil {
		return converted{}, err
	}
	boxes := docling.LayerBoxes(doc)
	blocks, err := w.client.Lines(ctx, pdf, boxes)
	if err != nil {
		return converted{}, err
	}
	layer, err := docling.Layer(boxes, blocks)
	if err != nil {
		return converted{}, err
	}
	return converted{doc: doc, layer: layer}, nil
}

// pdfOutcome says what an error from converting or mapping a PDF means on
// attempt: the code to fail the import with now, if any, and the error for
// the jobs port. A refused PDF fails at once; docling being unavailable is
// retried, and fails the import with pdf_conversion_unavailable on the last
// attempt.
func pdfOutcome(cause error, attempt int) (code string, err error) {
	var refused *docling.RefusedError
	switch {
	case errors.As(cause, &refused):
		return refused.Code, jobs.Permanent(cause)
	case errors.Is(cause, docling.ErrNoText):
		return docling.CodeScanned, jobs.Permanent(cause)
	case !errors.Is(cause, docling.ErrUnavailable):
		return docling.CodeFailed, jobs.Permanent(cause)
	case attempt >= maxAttempts:
		return codeUnavailable, cause
	default:
		return "", cause
	}
}

// codeUnavailable fails a PDF import whose last attempt found docling or
// pdftext unavailable.
const codeUnavailable = "pdf_conversion_unavailable"

// BlobArgs are a blob deletion job's payload: keys to delete, or a prefix
// to delete everything under.
type BlobArgs struct {
	Keys   []string `json:"keys,omitempty"`
	Prefix string   `json:"prefix,omitempty"`
}

// sweepAge is how old an unreferenced blob must be before the sweep deletes
// it: an upload's blob lands a moment before its import commits.
const sweepAge = time.Hour

// sweepEvery spaces the sweeps.
const sweepEvery = 24 * time.Hour

// Blobs runs blob deletion and the sweep of leftovers. Deleting a missing
// blob succeeds, so each job may run again.
type Blobs struct {
	store  *store.Store
	blobs  blob.Store
	queue  jobs.Enqueuer
	logger *slog.Logger
}

// NewBlobs returns a Blobs over blobs, enqueuing the next sweep on queue.
func NewBlobs(s *store.Store, blobs blob.Store, queue jobs.Enqueuer, logger *slog.Logger) *Blobs {
	return &Blobs{store: s, blobs: blobs, queue: queue, logger: logger}
}

// HandleDelete deletes a job's keys, or everything under its prefix.
func (b *Blobs) HandleDelete(ctx context.Context, d jobs.Delivery) error {
	var args BlobArgs
	if err := json.Unmarshal(d.Job.Payload, &args); err != nil {
		return jobs.Permanent(fmt.Errorf("blob deletion payload: %w", err))
	}
	if args.Prefix != "" {
		if !strings.HasPrefix(args.Prefix, "pdf/") || !strings.HasSuffix(args.Prefix, "/") {
			return jobs.Permanent(fmt.Errorf("blob deletion of prefix %q, outside pdf/<owner>/", args.Prefix))
		}
		return blob.DeletePrefix(ctx, b.blobs, args.Prefix)
	}
	for _, key := range args.Keys {
		if err := b.blobs.Delete(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

// Sweep deletes every blob under pdf/ older than an hour that no PDF
// snapshot holds and no unfinished import will read: blobs of deleted
// accounts and documents a deletion job missed, and uploads whose import
// never committed or failed.
func (b *Blobs) Sweep(ctx context.Context) (int, error) {
	var stale []string
	for info, err := range b.blobs.List(ctx, "pdf/") {
		if err != nil {
			return 0, err
		}
		if time.Since(info.Modified) >= sweepAge {
			stale = append(stale, info.Key)
		}
	}
	deleted := 0
	for len(stale) > 0 {
		batch := stale[:min(len(stale), 500)]
		stale = stale[len(batch):]
		used, err := b.store.BlobKeysInUse(ctx, batch)
		if err != nil {
			return deleted, err
		}
		for _, key := range batch {
			if slices.Contains(used, key) {
				continue
			}
			if err := b.blobs.Delete(ctx, key); err != nil {
				return deleted, err
			}
			deleted++
		}
	}
	return deleted, nil
}

// HandleSweep sweeps, then enqueues the next sweep a day later.
func (b *Blobs) HandleSweep(ctx context.Context, _ jobs.Delivery) error {
	n, err := b.Sweep(ctx)
	if err != nil {
		return err
	}
	b.logger.InfoContext(ctx, "blob sweep", "deleted", n)
	return b.ScheduleSweep(ctx, sweepEvery)
}

// ScheduleSweep enqueues a sweep after delay. Its key is the day it runs, so
// a worker that starts twice in a day sweeps once.
func (b *Blobs) ScheduleSweep(ctx context.Context, delay time.Duration) error {
	day := time.Now().Add(delay).UTC().Format(time.DateOnly)
	return b.store.InTx(ctx, func(_ *store.Store, raw pgx.Tx) error {
		return b.queue.EnqueueTx(ctx, raw, jobs.Job{Kind: SweepBlobsKind, Key: "sweep-" + day, Payload: []byte("{}"), Delay: delay})
	})
}

// Original opens the PDF owner's document id was made from, with its
// filename. A document that isn't owner's, or isn't from a PDF, is
// store.ErrNotFound; so is one whose blob is gone. The caller closes the
// object's body.
func (s *Service) Original(ctx context.Context, owner, id uuid.UUID) (string, *blob.Object, error) {
	orig, err := s.store.DocumentOriginal(ctx, owner, id)
	if err != nil {
		return "", nil, err
	}
	obj, err := s.blobs.Get(ctx, orig.BlobKey)
	if errors.Is(err, blob.ErrNotFound) {
		return "", nil, fmt.Errorf("original of document %s: %w", id, store.ErrNotFound)
	}
	if err != nil {
		return "", nil, err
	}
	return orig.Filename, obj, nil
}
