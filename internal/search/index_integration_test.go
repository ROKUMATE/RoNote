//go:build integration

package search

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/rokumate/ronote/internal/annotation"
	"github.com/rokumate/ronote/internal/doccontent"
	"github.com/rokumate/ronote/internal/embed"
	"github.com/rokumate/ronote/internal/ir"
	"github.com/rokumate/ronote/internal/jobs"
	"github.com/rokumate/ronote/internal/jobs/riverjobs"
	"github.com/rokumate/ronote/internal/store"
)

var discard = slog.New(slog.DiscardHandler)

// firstParagraph returns the index of content's first top-level paragraph.
func firstParagraph(t *testing.T, content *doccontent.Doc) int {
	t.Helper()
	for i, b := range content.Blocks {
		if b.Kind == ir.KindParagraph {
			return i
		}
	}
	t.Fatal("no paragraph")
	return -1
}

// TestReindexEmbedsOnlyWhatChanged indexes Effective Go, edits one
// paragraph, and indexes again: only the chunk holding it is embedded.
func TestReindexEmbedsOnlyWhatChanged(t *testing.T) {
	testDB.Reset(t)
	st := store.New(testDB.Pool)
	ctx := t.Context()
	ada := newUser(t, st)
	doc := importDoc(t, st, ada, fixture(t, "real-go-effective-go"))
	e := &counting{}
	ix := NewIndexer(st, e, discard)

	first, err := ix.Index(ctx, ada, doc)
	if err != nil {
		t.Fatal(err)
	}
	if first.Chunks < 5 || first.Hits != 0 || first.Sent != first.Chunks {
		t.Fatalf("first index: %+v, want every chunk embedded", first)
	}

	state, err := st.GetIndexState(ctx, ada, doc)
	if err != nil {
		t.Fatal(err)
	}
	if state.IndexedSeq != state.IndexSeq {
		t.Errorf("indexed sequence %d, want the document's %d", state.IndexedSeq, state.IndexSeq)
	}
	content := state.Content
	p := firstParagraph(t, content)
	content.Blocks[p].Content = []ir.Inline{{Text: "A sentence the student rewrote."}}
	edited := true
	content.Blocks[p].Edited = &edited
	got, err := st.GetDocument(ctx, ada, doc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveDocument(ctx, ada, doc, content, got.Version); err != nil {
		t.Fatal(err)
	}

	second, err := ix.Index(ctx, ada, doc)
	if err != nil {
		t.Fatal(err)
	}
	if second.Sent != 1 || second.Hits != second.Chunks-1 {
		t.Errorf("after one edit: %+v, want one chunk embedded and the rest from the cache", second)
	}
	t.Logf("re-index after one edit: %d of %d chunks from the cache (%.0f%%)", second.Hits, second.Chunks, 100*float64(second.Hits)/float64(second.Chunks))

	if err := st.DeleteUser(ctx, ada); err != nil {
		t.Fatal(err)
	}
	if n, err := st.CountChunks(ctx, ada, doc); err != nil || n != 0 {
		t.Errorf("%d chunks after the account went (%v), want 0", n, err)
	}
}

// broken is an embedder that fails with err.
type broken struct {
	embed.Fake
	err error
}

func (b broken) Embed(context.Context, []string) ([][]float32, error) { return nil, b.err }

func TestIndexJob(t *testing.T) {
	tests := []struct {
		name      string
		embedder  embed.Embedder
		seq       func(current int64) int64
		gone      bool
		wantErr   bool
		permanent bool
		indexed   bool
	}{
		{name: "the current sequence indexes", embedder: embed.Fake{}, seq: func(c int64) int64 { return c }, indexed: true},
		{name: "a stale job does nothing", embedder: &counting{}, seq: func(c int64) int64 { return c - 1 }},
		{name: "a gone document is done", embedder: embed.Fake{}, seq: func(c int64) int64 { return c }, gone: true},
		{name: "an embedder down retries", embedder: broken{err: errors.New("connection refused")}, seq: func(c int64) int64 { return c }, wantErr: true},
		{name: "an embedder refusing stops", embedder: broken{err: embed.ErrRejected}, seq: func(c int64) int64 { return c }, wantErr: true, permanent: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDB.Reset(t)
			st := store.New(testDB.Pool)
			ctx := t.Context()
			ada := newUser(t, st)
			doc := importDoc(t, st, ada, fixture(t, "real-python-controlflow"))
			state, err := st.GetIndexState(ctx, ada, doc)
			if err != nil {
				t.Fatal(err)
			}
			if tt.gone {
				if _, err := testDB.Pool.Exec(ctx, "DELETE FROM documents WHERE id = $1", doc); err != nil {
					t.Fatal(err)
				}
			}
			job := IndexJob(ada, doc, tt.seq(state.IndexSeq), tt.embedder.Model())

			err = NewIndexer(st, tt.embedder, discard).Handle(ctx, jobs.Delivery{Job: job, Attempt: 1})

			if (err != nil) != tt.wantErr || jobs.IsPermanent(err) != tt.permanent {
				t.Fatalf("Handle() = %v, want error %v, permanent %v", err, tt.wantErr, tt.permanent)
			}
			if c, ok := tt.embedder.(*counting); ok && len(c.sent) > 0 {
				t.Errorf("a stale job embedded %d texts", len(c.sent))
			}
			n, _ := st.CountChunks(ctx, ada, doc)
			if indexed := n > 0; indexed != tt.indexed {
				t.Errorf("%d chunks, want indexed = %v", n, tt.indexed)
			}
		})
	}
}

// hookLog records the change hook's calls.
type hookLog struct {
	seqs []int64
	fail error
}

func (h *hookLog) hook(_ context.Context, tx pgx.Tx, _, _ uuid.UUID, seq int64) error {
	if tx == nil {
		return errors.New("the hook ran outside a transaction")
	}
	h.seqs = append(h.seqs, seq)
	return h.fail
}

// TestChangesEnqueueIndexing makes each change search sees, and some it
// doesn't, and checks which call the hook, in their transaction.
func TestChangesEnqueueIndexing(t *testing.T) {
	testDB.Reset(t)
	log := &hookLog{}
	st := store.New(testDB.Pool).OnChange(log.hook)
	ctx := t.Context()
	ada := newUser(t, st)
	doc := importDoc(t, st, ada, fixture(t, "real-python-controlflow"))
	calls := func() int { return len(log.seqs) }
	if calls() != 1 {
		t.Fatalf("a new document called the hook %d times, want once", calls())
	}
	state, err := st.GetIndexState(ctx, ada, doc)
	if err != nil {
		t.Fatal(err)
	}
	p := state.Content.Blocks[firstParagraph(t, state.Content)]
	text, _ := doccontent.BlockText(state.Content, p.ID)
	sel := annotation.Selector{Position: annotation.Position{Start: 0, End: 4}, Quote: annotation.Quote{Exact: text[:4]}}

	plain, err := st.CreateAnnotation(ctx, ada, doc, store.NewAnnotation{BlockID: p.ID, Kind: "highlight", Selector: sel})
	if err != nil || calls() != 1 {
		t.Fatalf("a highlight without a note: %v, %d calls; want none", err, calls())
	}
	note := "loops come next"
	if _, err := st.UpdateAnnotation(ctx, ada, plain.ID, store.AnnotationChange{Version: plain.Version, Note: &note}); err != nil || calls() != 2 {
		t.Fatalf("adding a note: %v, %d calls; want 2", err, calls())
	}
	noted, err := st.CreateAnnotation(ctx, ada, doc, store.NewAnnotation{BlockID: p.ID, Kind: "highlight", Selector: sel, Note: "see 4.2"})
	if err != nil || calls() != 3 {
		t.Fatalf("a highlight with a note: %v, %d calls; want 3", err, calls())
	}
	if err := st.DeleteAnnotation(ctx, ada, noted.ID); err != nil || calls() != 4 {
		t.Fatalf("deleting a note: %v, %d calls; want 4", err, calls())
	}
	got, err := st.GetDocument(ctx, ada, doc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveDocument(ctx, ada, doc, got.Content, got.Version); err != nil || calls() != 5 {
		t.Fatalf("saving: %v, %d calls; want 5", err, calls())
	}
	for i := 1; i < len(log.seqs); i++ {
		if log.seqs[i] != log.seqs[i-1]+1 {
			t.Errorf("sequences %v, want each one more", log.seqs)
		}
	}

	// A hook that fails takes its change with it.
	log.fail = errors.New("queue down")
	if _, err := st.SaveDocument(ctx, ada, doc, got.Content, got.Version+1); err == nil {
		t.Fatal("a save whose hook failed succeeded")
	}
	after, err := st.GetDocument(ctx, ada, doc)
	if err != nil || after.Version != got.Version+1 {
		t.Errorf("version %d after a failed hook (%v), want %d: the save rolled back", after.Version, err, got.Version+1)
	}
}

func TestBackfill(t *testing.T) {
	testDB.Reset(t)
	st := store.New(testDB.Pool)
	ctx := t.Context()
	ada := newUser(t, st)
	fresh := importDoc(t, st, ada, fixture(t, "real-python-controlflow"))
	current := importDoc(t, st, ada, fixture(t, "real-kubernetes-pods"))
	otherModel := importDoc(t, st, ada, fixture(t, "real-go-effective-go"))
	if _, err := NewIndexer(st, embed.Fake{}, discard).Index(ctx, ada, current); err != nil {
		t.Fatal(err)
	}
	if _, err := NewIndexer(st, renamed{"older-model"}, discard).Index(ctx, ada, otherModel); err != nil {
		t.Fatal(err)
	}
	queue, err := riverjobs.New(testDB.Pool, riverjobs.Options{Logger: discard, Kinds: map[string]jobs.Options{JobKind: JobOptions}})
	if err != nil {
		t.Fatal(err)
	}

	for range 2 { // a second backfill enqueues nothing new
		if _, err := Backfill(ctx, st, queue, embed.FakeModel, time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := testDB.Pool.Query(ctx, "SELECT args ->> 'key' FROM river_job WHERE kind = $1 ORDER BY id", JobKind)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	var docs []string
	for _, k := range keys {
		docs = append(docs, k[:36])
	}
	slices.Sort(docs)
	want := []string{fresh.String(), otherModel.String()}
	slices.Sort(want)
	if !slices.Equal(docs, want) {
		t.Errorf("backfill enqueued %q, want the unindexed and the other model's documents once each", keys)
	}
}

// renamed is the fake embedder under another model's name.
type renamed struct{ model string }

func (r renamed) Model() string { return r.model }

func (renamed) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return embed.Fake{}.Embed(ctx, texts)
}
