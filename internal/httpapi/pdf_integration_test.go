//go:build integration

package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/rokumate/ronote/internal/blob/memblob"
	"github.com/rokumate/ronote/internal/importer"
	"github.com/rokumate/ronote/internal/ir"
	"github.com/rokumate/ronote/internal/jobs/riverjobs"
	"github.com/rokumate/ronote/internal/pgtest"
	"github.com/rokumate/ronote/internal/store"
)

// TestOriginalIsTheOwners serves a PDF document's original to its owner
// alone: another user, and a web page's document, get 404.
func TestOriginalIsTheOwners(t *testing.T) {
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
	blobs := memblob.New()
	const pdf, sha = "%PDF-1.7 ada's notes", "4e0b3e6b1b5ef7d7c8b1c2c7f8f0e6b6a3f8d1c0e9b2a7d6c5b4a3f2e1d0c9b8"
	key := importer.PDFKey(ada.ID, sha)
	if err := blobs.Put(ctx, key, strings.NewReader(pdf), int64(len(pdf))); err != nil {
		t.Fatal(err)
	}
	src, snap, err := st.SavePDFSource(ctx, store.PDFSource{OwnerID: ada.ID, SHA256: sha, BlobKey: key, ConvertedAt: t0})
	if err != nil {
		t.Fatal(err)
	}
	doc := &ir.Doc{
		IRVersion: ir.Version, Title: "Notes", Extractor: ir.Extractor{Name: "docling", Version: "1.38.2+map1"},
		Source: ir.Source{FetchedAt: t0, BodySHA256: sha, PDF: &ir.PDF{Filename: "Notes.pdf", Pages: 1}},
		Blocks: []ir.Block{{Kind: ir.KindParagraph, Page: 1, Content: []ir.Inline{{Text: "Hello."}}}},
	}
	ir.AssignIDs(doc.Blocks)
	ext, err := st.SaveExtraction(ctx, snap, doc)
	if err != nil {
		t.Fatal(err)
	}
	id, _, err := st.CreateDocumentIfAbsent(ctx, store.NewDocument{OwnerID: ada.ID, SourceID: src, ExtractionID: ext.ID, Content: ext.Doc})
	if err != nil {
		t.Fatal(err)
	}
	const url = "https://docs.example.com/page"
	web := &ir.Doc{
		IRVersion: ir.Version, Title: "Page", Extractor: ir.Extractor{Name: "generic", Version: "2"},
		Source: ir.Source{URL: url, FinalURL: url, FetchedAt: t0, BodySHA256: "ab"},
		Blocks: []ir.Block{{Kind: ir.KindParagraph, Content: []ir.Inline{{Text: "Hi."}}}},
	}
	ir.AssignIDs(web.Blocks)
	webID, err := st.AddDocument(ctx, ada.ID, url, web)
	if err != nil {
		t.Fatal(err)
	}

	queue, err := riverjobs.New(db.Pool, riverjobs.Options{Logger: discardLogger})
	if err != nil {
		t.Fatal(err)
	}
	backend := integrationBackend{Store: st, Service: importer.NewService(st, queue, importer.Quota{ImportsPerDay: 10, Documents: 10}, blobs)}
	o := Options{Logger: discardLogger, DB: db.Pool, Backend: backend}
	adaAPI, graceAPI := signedInAs(t, st, o, ada.ID), signedInAs(t, st, o, grace.ID)
	target := "/api/v1/documents/" + id.String() + "/original"

	rec := apiRequest(t, adaAPI, http.MethodGet, target, "")
	if rec.Code != http.StatusOK || rec.Body.String() != pdf || rec.Header().Get("Content-Security-Policy") != "sandbox; default-src 'none'" {
		t.Errorf("owner's original = %d %q, CSP %q", rec.Code, rec.Body, rec.Header().Get("Content-Security-Policy"))
	}
	if rec := apiRequest(t, graceAPI, http.MethodGet, target, ""); rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "ada") {
		t.Errorf("another user's request for the original = %d %q, want 404", rec.Code, rec.Body)
	}
	if rec := apiRequest(t, adaAPI, http.MethodGet, "/api/v1/documents/"+webID.String()+"/original", ""); rec.Code != http.StatusNotFound {
		t.Errorf("a web page's original = %d, want 404", rec.Code)
	}

	got := decodeJSON[struct {
		SourceURL string  `json:"source_url"`
		PDF       *ir.PDF `json:"pdf"`
	}](t, apiRequest(t, adaAPI, http.MethodGet, "/api/v1/documents/"+id.String(), ""))
	if got.SourceURL != "" || got.PDF == nil || *got.PDF != (ir.PDF{Filename: "Notes.pdf", Pages: 1}) {
		t.Errorf("document source = %q, %+v", got.SourceURL, got.PDF)
	}
}
