//go:build integration

package httpapi

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/annotation"
	"github.com/rokumate/ronote/internal/ir"
	"github.com/rokumate/ronote/internal/pgtest"
	"github.com/rokumate/ronote/internal/store"
)

// TestDeleteDocumentThroughAPI deletes a document with its notes and the
// conversations about it. Only its owner can; the import that made it stays,
// without it, and still counts toward the quota.
func TestDeleteDocumentThroughAPI(t *testing.T) {
	db := pgtest.New(t)
	ctx := t.Context()
	st := store.New(db.Pool)
	ada, err := st.CreateUser(ctx, "ada@example.com", "Ada")
	if err != nil {
		t.Fatal(err)
	}
	grace, err := st.CreateUser(ctx, "grace@example.com", "Grace")
	if err != nil {
		t.Fatal(err)
	}
	const url = "https://docs.example.com/page"
	page := &ir.Doc{
		IRVersion: ir.Version, Title: "Page", Extractor: ir.Extractor{Name: "generic", Version: "2"},
		Source: ir.Source{URL: url, FinalURL: url, FetchedAt: t0, BodySHA256: "ab"},
		Blocks: []ir.Block{{Kind: ir.KindParagraph, Content: []ir.Inline{{Text: "Hello, world."}}}},
	}
	ir.AssignIDs(page.Blocks)
	doc, err := st.AddDocument(ctx, ada.ID, url, page)
	if err != nil {
		t.Fatal(err)
	}
	imp, err := st.CreateImport(ctx, ada.ID, url, url)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.FinishImport(ctx, ada.ID, imp.ID, doc); err != nil {
		t.Fatal(err)
	}
	sel := annotation.Selector{Position: annotation.Position{Start: 0, End: 5}, Quote: annotation.Quote{Exact: "Hello"}}
	if _, err := st.CreateAnnotation(ctx, ada.ID, doc, store.NewAnnotation{BlockID: page.Blocks[0].ID, Kind: "highlight", Selector: sel, Note: "mine"}); err != nil {
		t.Fatal(err)
	}
	about, err := st.CreateConversation(ctx, ada.ID, doc)
	if err != nil {
		t.Fatal(err)
	}
	library, err := st.CreateConversation(ctx, ada.ID, uuid.Nil)
	if err != nil {
		t.Fatal(err)
	}
	o := Options{Logger: discardLogger, DB: db.Pool, Backend: integrationBackend{Store: st}}
	adaAPI, graceAPI := signedInAs(t, st, o, ada.ID), signedInAs(t, st, o, grace.ID)
	target := "/api/v1/documents/" + doc.String()
	status := func(h http.Handler, method, target string) int {
		t.Helper()
		return apiRequest(t, h, method, target, "").Code
	}
	count := func(query string) int {
		t.Helper()
		var n int
		if err := db.Pool.QueryRow(ctx, query, doc).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	if got := status(graceAPI, http.MethodDelete, target); got != http.StatusNotFound {
		t.Errorf("another user's delete = %d, want 404", got)
	}
	if got := status(adaAPI, http.MethodGet, target); got != http.StatusOK {
		t.Fatalf("after another user's delete, GET = %d, want 200", got)
	}
	if got := status(adaAPI, http.MethodDelete, "/api/v1/documents/"+uuid.NewString()); got != http.StatusNotFound {
		t.Errorf("delete of a missing document = %d, want 404", got)
	}

	if got := status(adaAPI, http.MethodDelete, target); got != http.StatusNoContent {
		t.Fatalf("delete = %d, want 204", got)
	}
	if got := status(adaAPI, http.MethodGet, target); got != http.StatusNotFound {
		t.Errorf("GET after delete = %d, want 404", got)
	}
	if got := status(adaAPI, http.MethodDelete, target); got != http.StatusNotFound {
		t.Errorf("second delete = %d, want 404", got)
	}
	if n := count(`SELECT count(*) FROM annotations WHERE document_id = $1`); n != 0 {
		t.Errorf("%d annotations left", n)
	}
	if _, err := st.GetConversation(ctx, ada.ID, about.ID); err == nil {
		t.Error("the conversation about the document is still there")
	}
	if _, err := st.GetConversation(ctx, ada.ID, library.ID); err != nil {
		t.Errorf("the conversation over the library went too: %v", err)
	}
	got, err := st.GetImport(ctx, ada.ID, imp.ID)
	if err != nil {
		t.Fatalf("the import went with the document: %v", err)
	}
	if got.Status != store.ImportDone || got.DocumentID != uuid.Nil {
		t.Errorf("import after delete: %s with document %s, want done without one", got.Status, got.DocumentID)
	}
	usage, err := st.QuotaUsage(ctx, ada.ID, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if usage.RecentImports != 1 || usage.Documents != 0 {
		t.Errorf("quota after delete: %d recent imports, %d documents; want 1 and 0", usage.RecentImports, usage.Documents)
	}
}
