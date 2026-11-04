//go:build integration

package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/rokumate/ronote/internal/doccontent"
)

// fixture is one user with a source, a snapshot and an extraction to make
// documents from.
type fixture struct {
	s          *Store
	ada, grace User
	source     Source
	extraction Extraction
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	testDB.Reset(t)
	s := New(testDB.Pool)
	ctx := t.Context()
	f := fixture{s: s}
	var err error
	if f.ada, err = s.UpsertUserByEmail(ctx, "ada@example.com", "Ada"); err != nil {
		t.Fatal(err)
	}
	if f.grace, err = s.UpsertUserByEmail(ctx, "grace@example.com", "Grace"); err != nil {
		t.Fatal(err)
	}
	f.source = testSource(t, s)
	snap, err := s.SaveSnapshot(ctx, Snapshot{SourceID: f.source.ID, BodySHA256: hash(1), Body: []byte("x"), ContentType: "text/html", FetchedAt: t0})
	if err != nil {
		t.Fatal(err)
	}
	if f.extraction, err = s.SaveExtraction(ctx, snap, extracted("2", "Guide")); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f fixture) document(t *testing.T, owner User) uuid.UUID {
	t.Helper()
	id, _, err := f.s.CreateDocumentIfAbsent(t.Context(), NewDocument{
		OwnerID: owner.ID, SourceID: f.source.ID, ExtractionID: f.extraction.ID, Content: f.extraction.Doc,
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestUpsertUserByEmail(t *testing.T) {
	testDB.Reset(t)
	s := New(testDB.Pool)
	first, err := s.UpsertUserByEmail(t.Context(), "dev@ronote.local", "dev")
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.UpsertUserByEmail(t.Context(), "DEV@ronote.local", "other")
	if err != nil || again.ID != first.ID || again.DisplayName != "dev" {
		t.Errorf("second upsert = %+v, %v; want the first user unchanged", again, err)
	}
}

func TestDocuments(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()

	id, created, err := f.s.CreateDocumentIfAbsent(ctx, NewDocument{OwnerID: f.ada.ID, SourceID: f.source.ID, ExtractionID: f.extraction.ID, Content: f.extraction.Doc})
	if err != nil || !created {
		t.Fatalf("first create = %v, %v, %v", id, created, err)
	}
	again, created, err := f.s.CreateDocumentIfAbsent(ctx, NewDocument{OwnerID: f.ada.ID, SourceID: f.source.ID, ExtractionID: f.extraction.ID, Content: f.extraction.Doc})
	if err != nil || created || again != id {
		t.Errorf("second create = %v, %v, %v; want the existing %v, not created", again, created, err, id)
	}

	doc, err := f.s.GetDocument(ctx, f.ada.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Title != "Guide" || doc.Version != 1 || doc.SourceURL != f.source.CanonicalURL || doccontent.Validate(doc.Content) != nil {
		t.Errorf("document = %+v", doc.DocumentSummary)
	}
	if got, err := f.s.FindOwnerDocumentForURL(ctx, f.ada.ID, f.source.CanonicalURL); err != nil || got != id {
		t.Errorf("FindOwnerDocumentForURL(canonical) = %v, %v", got, err)
	}
	if got, err := f.s.FindOwnerDocumentForURL(ctx, f.ada.ID, f.source.FinalURL); err != nil || got != id {
		t.Errorf("FindOwnerDocumentForURL(final) = %v, %v", got, err)
	}

	// An earlier import of another URL that led to the document finds it too.
	imp, err := f.s.CreateImport(ctx, f.ada.ID, "https://old.example.com/a", "https://old.example.com/a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.FinishImport(ctx, f.ada.ID, imp.ID, id); err != nil {
		t.Fatal(err)
	}
	if got, err := f.s.FindOwnerDocumentForURL(ctx, f.ada.ID, "https://old.example.com/a"); err != nil || got != id {
		t.Errorf("FindOwnerDocumentForURL(earlier import) = %v, %v", got, err)
	}
}

func TestImportStatus(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	doc := f.document(t, f.ada)
	imp, err := f.s.CreateImport(ctx, f.ada.ID, "https://docs.example.com/a#intro", "https://docs.example.com/a")
	if err != nil {
		t.Fatal(err)
	}
	if imp.Status != ImportQueued || imp.DocumentID != uuid.Nil {
		t.Fatalf("new import = %+v", imp)
	}
	for _, status := range []ImportStatus{ImportFetching, ImportExtracting} {
		if imp, err = f.s.SetImportStatus(ctx, f.ada.ID, imp.ID, status); err != nil || imp.Status != status {
			t.Fatalf("SetImportStatus(%s) = %v, %v", status, imp.Status, err)
		}
	}
	if imp, err = f.s.FinishImport(ctx, f.ada.ID, imp.ID, doc); err != nil || imp.Status != ImportDone || imp.DocumentID != doc {
		t.Fatalf("FinishImport() = %+v, %v", imp, err)
	}
	if _, err := f.s.FinishImport(ctx, f.ada.ID, imp.ID, doc); err != nil {
		t.Errorf("finishing again with the same document = %v, want success", err)
	}
	if _, err := f.s.SetImportStatus(ctx, f.ada.ID, imp.ID, ImportFetching); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetImportStatus() on a done import err = %v, want ErrNotFound", err)
	}
	if _, err := f.s.FailImport(ctx, f.ada.ID, imp.ID, "timeout"); !errors.Is(err, ErrNotFound) {
		t.Errorf("FailImport() on a done import err = %v, want ErrNotFound", err)
	}

	failed, err := f.s.CreateImport(ctx, f.ada.ID, "https://docs.example.com/b", "https://docs.example.com/b")
	if err != nil {
		t.Fatal(err)
	}
	if failed, err = f.s.FailImport(ctx, f.ada.ID, failed.ID, "disallowed_by_robots"); err != nil || failed.ErrorCode != "disallowed_by_robots" {
		t.Fatalf("FailImport() = %+v, %v", failed, err)
	}
	if _, err := f.s.FinishImport(ctx, f.ada.ID, failed.ID, doc); !errors.Is(err, ErrNotFound) {
		t.Errorf("FinishImport() on a failed import err = %v, want ErrNotFound", err)
	}
	if latest, err := f.s.LatestImportForDocument(ctx, f.ada.ID, doc); err != nil || latest.ID != imp.ID {
		t.Errorf("LatestImportForDocument() = %v, %v; want %v", latest.ID, err, imp.ID)
	}
}

// TestOwnerIsolation proves one user cannot read, list, find or change
// another user's documents and imports.
func TestOwnerIsolation(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	adaDoc := f.document(t, f.ada)
	adaImport, err := f.s.CreateImport(ctx, f.ada.ID, f.source.CanonicalURL, f.source.CanonicalURL)
	if err != nil {
		t.Fatal(err)
	}
	grace := f.grace.ID

	checks := []struct {
		name string
		call func() error
	}{
		{"GetDocument", func() error { _, err := f.s.GetDocument(ctx, grace, adaDoc); return err }},
		{"GetImport", func() error { _, err := f.s.GetImport(ctx, grace, adaImport.ID); return err }},
		{"FindOwnerDocumentForURL", func() error { _, err := f.s.FindOwnerDocumentForURL(ctx, grace, f.source.CanonicalURL); return err }},
		{"LatestImportForDocument", func() error { _, err := f.s.LatestImportForDocument(ctx, grace, adaDoc); return err }},
		{"SetImportStatus", func() error { _, err := f.s.SetImportStatus(ctx, grace, adaImport.ID, ImportFetching); return err }},
		{"FailImport", func() error { _, err := f.s.FailImport(ctx, grace, adaImport.ID, "x"); return err }},
		{"FinishImport", func() error { _, err := f.s.FinishImport(ctx, grace, adaImport.ID, adaDoc); return err }},
	}
	for _, c := range checks {
		if err := c.call(); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s on another user's row: err = %v, want ErrNotFound", c.name, err)
		}
	}
	if docs, err := f.s.ListDocuments(ctx, grace, nil, 10); err != nil || len(docs) != 0 {
		t.Errorf("ListDocuments() for another user = %d docs, %v; want none", len(docs), err)
	}
	if got, err := f.s.GetImport(ctx, f.ada.ID, adaImport.ID); err != nil || got.Status != ImportQueued {
		t.Errorf("the owner's import after the attempts = %+v, %v; want it unchanged", got, err)
	}

	// The database itself refuses to attach a user's document to another
	// user's import.
	graceImport, err := f.s.CreateImport(ctx, grace, f.source.CanonicalURL, f.source.CanonicalURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.FinishImport(ctx, grace, graceImport.ID, adaDoc); err == nil {
		t.Error("finishing an import with another user's document succeeded")
	}
}

func TestListDocumentsPages(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	var want []uuid.UUID
	for i := range 5 {
		src, err := f.s.UpsertSource(ctx, Source{CanonicalURL: fmt.Sprintf("https://docs.example.com/p%d", i), FinalURL: "https://docs.example.com/x", LastFetchedAt: t0})
		if err != nil {
			t.Fatal(err)
		}
		id, _, err := f.s.CreateDocumentIfAbsent(ctx, NewDocument{OwnerID: f.ada.ID, SourceID: src.ID, ExtractionID: f.extraction.ID, Content: f.extraction.Doc})
		if err != nil {
			t.Fatal(err)
		}
		want = append([]uuid.UUID{id}, want...) // newest first
	}

	var got []uuid.UUID
	var after *DocumentCursor
	for page := 0; page < 4; page++ {
		docs, err := f.s.ListDocuments(ctx, f.ada.ID, after, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range docs {
			got = append(got, d.ID)
		}
		if len(docs) < 2 {
			break
		}
		last := docs[len(docs)-1]
		after = &DocumentCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("pages gave %v, want %v", got, want)
	}
}

// TestImportStatusNotifies checks that creating an import and changing its
// status notify import_status, and only once the transaction commits.
func TestImportStatusNotifies(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	conn, err := pgx.Connect(ctx, testDB.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := conn.Close(context.WithoutCancel(ctx)); err != nil {
			t.Errorf("close listener: %v", err)
		}
	})
	if _, err := conn.Exec(ctx, "LISTEN import_status"); err != nil {
		t.Fatal(err)
	}

	var imp Import
	err = f.s.InTx(ctx, func(tx *Store, _ pgx.Tx) error {
		var err error
		if imp, err = tx.CreateImport(ctx, f.ada.ID, f.source.CanonicalURL, f.source.CanonicalURL); err != nil {
			return err
		}
		if _, err = tx.SetImportStatus(ctx, f.ada.ID, imp.ID, ImportFetching); err != nil {
			return err
		}
		if _, err = tx.SetImportStatus(ctx, f.ada.ID, imp.ID, ImportFetching); err != nil { // no change, no notification
			return err
		}
		waitCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		defer cancel()
		if n, err := conn.WaitForNotification(waitCtx); err == nil {
			return fmt.Errorf("notification before commit: %s", n.Payload)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []ImportStatus{ImportQueued, ImportFetching} {
		waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		n, err := conn.WaitForNotification(waitCtx)
		cancel()
		if err != nil {
			t.Fatalf("waiting for %s: %v", want, err)
		}
		var payload struct {
			ImportID uuid.UUID    `json:"import_id"`
			OwnerID  uuid.UUID    `json:"owner_id"`
			Status   ImportStatus `json:"status"`
		}
		if err := json.Unmarshal([]byte(n.Payload), &payload); err != nil {
			t.Fatal(err)
		}
		if n.Channel != "import_status" || payload.ImportID != imp.ID || payload.OwnerID != f.ada.ID || payload.Status != want {
			t.Errorf("notification = %s on %s, want %s for import %s", n.Payload, n.Channel, want, imp.ID)
		}
	}
	waitCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	if n, err := conn.WaitForNotification(waitCtx); err == nil {
		t.Errorf("unexpected extra notification %s", n.Payload)
	}
}

// TestResolveDocuments finds the owner's documents for many URLs at once,
// matched by a source's canonical or final URL or by an earlier import, and
// never another user's.
func TestResolveDocuments(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	doc := f.document(t, f.ada)
	imp, err := f.s.CreateImport(ctx, f.ada.ID, "https://docs.example.com/old-a", "https://docs.example.com/old-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.FinishImport(ctx, f.ada.ID, imp.ID, doc); err != nil {
		t.Fatal(err)
	}
	urls := []string{
		f.source.CanonicalURL,
		f.source.FinalURL,
		"https://docs.example.com/old-a",
		"https://docs.example.com/never-imported",
	}

	got, err := f.s.ResolveDocuments(ctx, f.ada.ID, urls)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]uuid.UUID{urls[0]: doc, urls[1]: doc, urls[2]: doc}
	if !maps.Equal(got, want) {
		t.Errorf("ResolveDocuments() for the owner = %v, want %v", got, want)
	}

	other, err := f.s.ResolveDocuments(ctx, f.grace.ID, urls)
	if err != nil || len(other) != 0 {
		t.Errorf("ResolveDocuments() for another user = %v, %v; want nothing", other, err)
	}
}
