package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/rokumate/ronote/internal/embed"
	"github.com/rokumate/ronote/internal/jobs"
	"github.com/rokumate/ronote/internal/store"
)

// JobKind is the job kind that indexes a document.
const JobKind = "index_document"

// JobOptions are index_document's: a failed embedder is retried with the
// adapter's backoff, a few minutes in all.
var JobOptions = jobs.Options{MaxAttempts: 6, Concurrency: 2, Timeout: 2 * time.Minute}

// collapse is how long an index job waits, so a burst of edits makes one.
const collapse = 5 * time.Second

// Args is an index job's payload.
type Args struct {
	OwnerID    uuid.UUID `json:"owner_id"`
	DocumentID uuid.UUID `json:"document_id"`
	// Seq is the document's index sequence when the job was made.
	Seq int64 `json:"seq"`
}

// IndexJob is the job that indexes owner's document at seq, by model:
// keyed so each change, and each model, indexes once.
func IndexJob(owner, document uuid.UUID, seq int64, model string) jobs.Job {
	payload, _ := json.Marshal(Args{OwnerID: owner, DocumentID: document, Seq: seq}) // a struct of UUIDs and an int marshals
	return jobs.Job{
		Kind:    JobKind,
		Key:     fmt.Sprintf("%s:%d:%s", document, seq, model),
		Payload: payload,
		Delay:   collapse,
	}
}

// EnqueueOnChange is the store's change hook in cmd/api and cmd/worker: it
// enqueues the document's indexing in the change's own transaction.
func EnqueueOnChange(q jobs.Enqueuer, model string) store.ChangeHook {
	return func(ctx context.Context, tx pgx.Tx, owner, document uuid.UUID, seq int64) error {
		return q.EnqueueTx(ctx, tx, IndexJob(owner, document, seq, model))
	}
}

// Indexer makes documents' chunks.
type Indexer struct {
	store    *store.Store
	embedder embed.Embedder
	logger   *slog.Logger
}

// NewIndexer returns an Indexer that reads and writes st and embeds with e.
func NewIndexer(st *store.Store, e embed.Embedder, logger *slog.Logger) *Indexer {
	return &Indexer{store: st, embedder: e, logger: logger}
}

// Report is what indexing one document did.
type Report struct {
	Chunks int
	// Hits counts chunks whose embedding the cache had, Sent the texts
	// sent to the embedder.
	Hits, Sent int
}

// Handle runs an index job. A job behind the document's sequence does
// nothing: the change that moved it enqueued a newer one. A gone document
// is done. An embedder that refuses the request stops the job.
func (ix *Indexer) Handle(ctx context.Context, d jobs.Delivery) error {
	var args Args
	if err := json.Unmarshal(d.Job.Payload, &args); err != nil {
		return jobs.Permanent(fmt.Errorf("index job payload: %w", err))
	}
	state, err := ix.store.GetIndexState(ctx, args.OwnerID, args.DocumentID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return nil
	case err != nil:
		return err
	case args.Seq < state.IndexSeq:
		return nil
	}
	_, err = ix.index(ctx, args.OwnerID, args.DocumentID, state)
	if errors.Is(err, embed.ErrRejected) {
		return jobs.Permanent(err)
	}
	return err
}

// Index makes owner's document's chunks from its current state.
func (ix *Indexer) Index(ctx context.Context, owner, document uuid.UUID) (Report, error) {
	state, err := ix.store.GetIndexState(ctx, owner, document)
	if err != nil {
		return Report{}, err
	}
	return ix.index(ctx, owner, document, state)
}

func (ix *Indexer) index(ctx context.Context, owner, document uuid.UUID, state store.IndexState) (Report, error) {
	annotations, err := ix.store.ListAnnotations(ctx, owner, document)
	if err != nil {
		return Report{}, err
	}
	var notes []Note
	for _, a := range annotations {
		if a.Note != "" {
			notes = append(notes, Note{BlockID: a.BlockID, Quote: a.Selector.Quote.Exact, Text: a.Note})
		}
	}
	chunks := Chunks(state.Title, state.Content, notes)
	inputs := make([]string, len(chunks))
	for i, c := range chunks {
		inputs[i] = c.Embed
	}
	embedded, err := EmbedCached(ctx, ix.store, ix.embedder, inputs)
	if err != nil {
		return Report{}, fmt.Errorf("index %s: %w", document, err)
	}
	rows := make([]store.NewChunk, len(chunks))
	for i, c := range chunks {
		rows[i] = store.NewChunk{
			BlockIDs: c.BlockIDs, HeadingPath: c.HeadingPath, Kind: string(c.Kind), Text: c.Text, Lang: c.Lang,
			Headings: c.Headings, Prose: c.Prose, Code: c.Code, Idents: IdentVector(c.Idents),
			Embedding: embedded.Vectors[i], Model: ix.embedder.Model(),
		}
	}
	err = ix.store.InTx(ctx, func(tx *store.Store, _ pgx.Tx) error {
		if err := tx.ReplaceChunks(ctx, owner, document, rows); err != nil {
			return err
		}
		// The sequence read before chunking: a change since moved it on,
		// and its own job indexes it.
		return tx.MarkIndexed(ctx, owner, document, state.IndexSeq)
	})
	if err != nil {
		return Report{}, err
	}
	report := Report{Chunks: len(chunks), Hits: embedded.Hits, Sent: embedded.Sent}
	ix.logger.LogAttrs(ctx, slog.LevelInfo, "document indexed",
		slog.String("document_id", document.String()), slog.Int("chunks", report.Chunks),
		slog.Int("cache_hits", report.Hits), slog.Int("embedded", report.Sent))
	return report, nil
}

// Backfill enqueues the indexing of every document that is behind or was
// embedded by another model, one each every, until done or ctx ends. The
// jobs' keys make a second backfill of the same document a no-op.
func Backfill(ctx context.Context, st *store.Store, q jobs.Enqueuer, model string, every time.Duration) (int, error) {
	behind, err := st.DocumentsBehind(ctx, model, 100_000)
	if err != nil {
		return 0, err
	}
	tick := time.NewTicker(every)
	defer tick.Stop()
	for i, b := range behind {
		if i > 0 {
			select {
			case <-ctx.Done():
				return i, ctx.Err()
			case <-tick.C:
			}
		}
		job := IndexJob(b.OwnerID, b.ID, b.IndexSeq, model)
		job.Delay = 0
		err := st.InTx(ctx, func(_ *store.Store, tx pgx.Tx) error { return q.EnqueueTx(ctx, tx, job) })
		if err != nil {
			return i, err
		}
	}
	return len(behind), nil
}
