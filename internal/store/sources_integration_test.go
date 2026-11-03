//go:build integration

package store

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/rokumate/ronote/internal/ir"
)

var t0 = time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)

func hash(b byte) string { return strings.Repeat(string("0123456789abcdef"[b%16]), 64) }

func testSource(t *testing.T, s *Store) Source {
	t.Helper()
	src, err := s.UpsertSource(t.Context(), Source{
		CanonicalURL: "https://docs.example.com/a", FinalURL: "https://docs.example.com/a?ref=nav",
		ETag: `"v1"`, LastFetchedAt: t0,
	})
	if err != nil {
		t.Fatal(err)
	}
	return src
}

func TestSources(t *testing.T) {
	testDB.Reset(t)
	s := New(testDB.Pool)
	ctx := t.Context()
	first := testSource(t, s)

	again, err := s.UpsertSource(ctx, Source{
		CanonicalURL: "https://docs.example.com/a", FinalURL: "https://docs.example.com/a",
		ETag: `"v2"`, LastModified: "Wed, 04 Nov 2026 09:00:00 GMT", LastFetchedAt: t0.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != first.ID || again.ETag != `"v2"` || !again.LastFetchedAt.Equal(t0.Add(time.Hour)) {
		t.Errorf("second upsert = %+v, want the same source with the new validators", again)
	}

	if got, err := s.FindSourceForURL(ctx, "https://docs.example.com/a"); err != nil || got.ID != first.ID {
		t.Errorf("FindSourceForURL(canonical URL) = %v, %v", got.ID, err)
	}
	if _, err := s.FindSourceForURL(ctx, "https://docs.example.com/missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("FindSourceForURL(missing) err = %v, want ErrNotFound", err)
	}

	if err := s.TouchSource(ctx, first.ID, t0.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.TouchSource(ctx, first.ID, t0); err != nil { // an older confirmation never wins
		t.Fatal(err)
	}
	if got, _ := s.FindSourceForURL(ctx, first.CanonicalURL); !got.LastFetchedAt.Equal(t0.Add(2 * time.Hour)) {
		t.Errorf("last_fetched_at = %v, want %v", got.LastFetchedAt, t0.Add(2*time.Hour))
	}
}

func TestFindSourceByFinalURL(t *testing.T) {
	testDB.Reset(t)
	s := New(testDB.Pool)
	src := testSource(t, s)
	got, err := s.FindSourceForURL(t.Context(), "https://docs.example.com/a?ref=nav")
	if err != nil || got.ID != src.ID {
		t.Errorf("FindSourceForURL(final URL) = %v, %v; want %v", got.ID, err, src.ID)
	}
}

func TestSnapshots(t *testing.T) {
	testDB.Reset(t)
	s := New(testDB.Pool)
	ctx := t.Context()
	src := testSource(t, s)
	if _, err := s.NewestSnapshot(ctx, src.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("NewestSnapshot() of a new source err = %v, want ErrNotFound", err)
	}

	body := []byte("<main><pre>x := 1\n</pre></main>")
	first, err := s.SaveSnapshot(ctx, Snapshot{SourceID: src.ID, BodySHA256: hash(1), Body: body, ContentType: "text/html", FetchedAt: t0})
	if err != nil {
		t.Fatal(err)
	}
	same, err := s.SaveSnapshot(ctx, Snapshot{SourceID: src.ID, BodySHA256: hash(1), Body: body, ContentType: "text/html", FetchedAt: t0.Add(time.Hour)})
	if err != nil || same != first {
		t.Fatalf("saving the same body again = %v, %v; want the existing %v", same, err, first)
	}
	newer, err := s.SaveSnapshot(ctx, Snapshot{SourceID: src.ID, BodySHA256: hash(2), Body: []byte("<main>v2</main>"), ContentType: "text/html", FetchedAt: t0.Add(time.Hour)})
	if err != nil || newer == first {
		t.Fatalf("saving a changed body = %v, %v; want a new snapshot", newer, err)
	}

	got, err := s.NewestSnapshot(ctx, src.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != newer || !bytes.Equal(got.Body, []byte("<main>v2</main>")) || got.BodySHA256 != hash(2) {
		t.Errorf("NewestSnapshot() = %v %q %s, want the changed body", got.ID, got.Body, got.BodySHA256)
	}
	var n int
	if err := testDB.Pool.QueryRow(ctx, "SELECT count(*) FROM source_snapshots").Scan(&n); err != nil || n != 2 {
		t.Errorf("snapshots stored = %d, %v; want 2", n, err)
	}
}

func extracted(version, title string) *ir.Doc {
	doc := &ir.Doc{
		IRVersion: ir.Version, Title: title,
		Source:    ir.Source{URL: "https://docs.example.com/a", FinalURL: "https://docs.example.com/a", FetchedAt: t0, BodySHA256: hash(1)},
		Extractor: ir.Extractor{Name: "generic", Version: version},
		Blocks:    []ir.Block{{Kind: ir.KindParagraph, Content: []ir.Inline{{Text: title}}}},
	}
	ir.AssignIDs(doc.Blocks)
	return doc
}

func TestExtractions(t *testing.T) {
	testDB.Reset(t)
	s := New(testDB.Pool)
	ctx := t.Context()
	src := testSource(t, s)
	snap, err := s.SaveSnapshot(ctx, Snapshot{SourceID: src.ID, BodySHA256: hash(1), Body: []byte("x"), ContentType: "text/html", FetchedAt: t0})
	if err != nil {
		t.Fatal(err)
	}

	first, err := s.SaveExtraction(ctx, snap, extracted("2", "First"))
	if err != nil {
		t.Fatal(err)
	}
	if first.Doc.Title != "First" || ir.Validate(first.Doc) != nil {
		t.Errorf("stored extraction = %+v", first.Doc)
	}
	reused, err := s.SaveExtraction(ctx, snap, extracted("2", "Second"))
	if err != nil {
		t.Fatal(err)
	}
	if reused.ID != first.ID || reused.Doc.Title != "First" {
		t.Errorf("same extractor version again = %v %q, want the stored %v %q", reused.ID, reused.Doc.Title, first.ID, "First")
	}
	newer, err := s.SaveExtraction(ctx, snap, extracted("3", "Third"))
	if err != nil || newer.ID == first.ID || newer.Doc.Title != "Third" {
		t.Errorf("a new extractor version = %v %v %q, want a new extraction", newer.ID, err, newer.Doc.Title)
	}
}

func TestInTx(t *testing.T) {
	testDB.Reset(t)
	s := New(testDB.Pool)
	ctx := t.Context()
	boom := errors.New("boom")

	err := s.InTx(ctx, func(tx *Store, _ pgx.Tx) error {
		if _, err := tx.UpsertSource(ctx, Source{CanonicalURL: "https://docs.example.com/rolled-back", FinalURL: "https://docs.example.com/rolled-back", LastFetchedAt: t0}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("InTx() = %v, want fn's error unchanged", err)
	}
	if _, err := s.FindSourceForURL(ctx, "https://docs.example.com/rolled-back"); !errors.Is(err, ErrNotFound) {
		t.Errorf("a source written in a failed transaction is visible: %v", err)
	}

	var inner error
	if err := s.InTx(ctx, func(tx *Store, _ pgx.Tx) error {
		inner = tx.InTx(ctx, func(*Store, pgx.Tx) error { return nil })
		_, err := tx.UpsertSource(ctx, Source{CanonicalURL: "https://docs.example.com/kept", FinalURL: "https://docs.example.com/kept", LastFetchedAt: t0})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if inner == nil {
		t.Error("InTx inside a transaction succeeded, want an error")
	}
	if _, err := s.FindSourceForURL(ctx, "https://docs.example.com/kept"); err != nil {
		t.Errorf("a committed source is missing: %v", err)
	}
}
