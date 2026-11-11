//go:build integration

package events

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/rokumate/ronote/internal/ir"
	"github.com/rokumate/ronote/internal/pgtest"
	"github.com/rokumate/ronote/internal/store"
)

// TestListenDeliversStatusChanges follows one import through its statuses
// as the worker would move it, over a real LISTEN connection.
func TestListenDeliversStatusChanges(t *testing.T) {
	db := pgtest.New(t)
	ctx := t.Context()
	s := store.New(db.Pool)
	ada, err := s.CreateUser(ctx, "ada@example.com", "Ada")
	if err != nil {
		t.Fatal(err)
	}

	b := NewBroker()
	listenCtx, stopListening := context.WithCancel(ctx)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		b.Listen(listenCtx, db.Pool, slog.New(slog.DiscardHandler))
	}()

	// Subscribe to a placeholder first: the resync sent once LISTEN is in
	// place says the listener is ready.
	ready, cancelReady := b.Subscribe(uuid.Nil)
	select {
	case <-ready:
	case <-time.After(10 * time.Second):
		t.Fatal("the listener did not start")
	}
	cancelReady()

	// Subscribe before the new import commits, so its queued event, sent
	// at commit, cannot be missed.
	var imp store.Import
	var updates <-chan Event
	var unsubscribe func()
	err = s.InTx(ctx, func(tx *store.Store, _ pgx.Tx) error {
		var err error
		imp, err = tx.CreateImport(ctx, ada.ID, "https://docs.example.com/a", "https://docs.example.com/a")
		updates, unsubscribe = b.Subscribe(imp.ID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []store.ImportStatus{store.ImportFetching, store.ImportExtracting} {
		if _, err := s.SetImportStatus(ctx, ada.ID, imp.ID, status); err != nil {
			t.Fatal(err)
		}
	}
	doc := finishWithDocument(t, s, ada, imp)

	for _, want := range []Event{
		{ImportID: imp.ID, OwnerID: ada.ID, Status: "queued"},
		{ImportID: imp.ID, OwnerID: ada.ID, Status: "fetching"},
		{ImportID: imp.ID, OwnerID: ada.ID, Status: "extracting"},
		{ImportID: imp.ID, OwnerID: ada.ID, Status: "done", DocumentID: doc},
	} {
		select {
		case got := <-updates:
			if got != want {
				t.Errorf("event = %+v, want %+v", got, want)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("no %s event", want.Status)
		}
	}

	unsubscribe()
	stopListening()
	<-stopped
	if n := b.subscribers(); n != 0 {
		t.Errorf("subscribers = %d, want 0", n)
	}
}

// finishWithDocument makes a document for imp's URL and marks imp done.
func finishWithDocument(t *testing.T, s *store.Store, owner store.User, imp store.Import) uuid.UUID {
	t.Helper()
	ctx := t.Context()
	at := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)
	src, err := s.UpsertSource(ctx, store.Source{CanonicalURL: imp.CanonicalURL, FinalURL: imp.CanonicalURL, LastFetchedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	hash := "0000000000000000000000000000000000000000000000000000000000000000"
	snap, err := s.SaveSnapshot(ctx, store.Snapshot{SourceID: src.ID, BodySHA256: hash, Body: []byte("x"), ContentType: "text/html", FetchedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	ext, err := s.SaveExtraction(ctx, snap, &ir.Doc{
		IRVersion: ir.Version, Title: "A", Extractor: ir.Extractor{Name: "generic", Version: "2"},
		Source: ir.Source{URL: imp.CanonicalURL, FinalURL: imp.CanonicalURL, FetchedAt: at, BodySHA256: hash},
	})
	if err != nil {
		t.Fatal(err)
	}
	doc, _, err := s.CreateDocumentIfAbsent(ctx, store.NewDocument{OwnerID: owner.ID, SourceID: src.ID, ExtractionID: ext.ID, Content: ext.Doc})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.FinishImport(ctx, owner.ID, imp.ID, doc); err != nil {
		t.Fatal(err)
	}
	return doc
}
