//go:build integration

package httpapi

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/doccontent"
	"github.com/rokumate/ronote/internal/fetch"
	"github.com/rokumate/ronote/internal/importer"
	"github.com/rokumate/ronote/internal/ir"
	"github.com/rokumate/ronote/internal/pgtest"
	"github.com/rokumate/ronote/internal/store"
)

// loopback resolves docs.test to 127.0.0.1, so no DNS is used.
type loopback struct{}

func (loopback) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	if host == "docs.test" {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

type integrationBackend struct {
	*store.Store
	*importer.Service
}

// TestAPIImportsEndToEnd imports a page through the API with a worker
// running, then checks another user sees none of it.
func TestAPIImportsEndToEnd(t *testing.T) {
	db := pgtest.New(t)
	ctx := t.Context()
	st := store.New(db.Pool)
	ada, err := st.UpsertUserByEmail(ctx, "ada@example.com", "Ada")
	if err != nil {
		t.Fatal(err)
	}
	grace, err := st.UpsertUserByEmail(ctx, "grace@example.com", "Grace")
	if err != nil {
		t.Fatal(err)
	}

	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/guide" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, `<html><head><title>Guide</title></head><body><main><h1>Guide</h1><pre><code>go run .
</code></pre></main></body></html>`)
	}))
	t.Cleanup(site.Close)
	_, port, _ := net.SplitHostPort(site.Listener.Addr().String())
	pageURL := "http://docs.test:" + port + "/guide"

	worker := importer.NewWorker(st, fetch.New(fetch.Options{AllowPrivateNetworks: true, Resolver: loopback{}}), time.Hour, discardLogger)
	client, err := importer.NewClient(db.Pool, worker, importer.ClientOptions{
		Logger: discardLogger, FetchCooldown: 20 * time.Millisecond, FetchPollInterval: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Start(context.WithoutCancel(ctx)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := client.Stop(stopCtx); err != nil {
			t.Errorf("stop River: %v", err)
		}
	})
	backend := integrationBackend{Store: st, Service: importer.NewService(st, client)}
	adaAPI := New(Options{Logger: discardLogger, DB: db.Pool, Backend: backend, DevUser: ada.ID})
	graceAPI := New(Options{Logger: discardLogger, DB: db.Pool, Backend: backend, DevUser: grace.ID})

	type importJSON struct {
		ID         string `json:"id"`
		Status     string `json:"status"`
		DocumentID string `json:"document_id"`
	}
	type importResponse struct {
		Import   importJSON     `json:"import"`
		Document map[string]any `json:"document"`
	}

	rec := apiRequest(t, adaAPI, http.MethodPost, "/api/v1/imports", `{"url":"`+pageURL+`"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST = %d %s, want 202", rec.Code, rec.Body)
	}
	imp := decodeJSON[importResponse](t, rec).Import
	deadline := time.Now().Add(20 * time.Second)
	for imp.Status != "done" && imp.Status != "failed" {
		if time.Now().After(deadline) {
			t.Fatalf("import still %s after 20s", imp.Status)
		}
		time.Sleep(25 * time.Millisecond)
		imp = decodeJSON[importResponse](t, apiRequest(t, adaAPI, http.MethodGet, "/api/v1/imports/"+imp.ID, "")).Import
	}
	if imp.Status != "done" {
		t.Fatalf("import = %+v, want done", imp)
	}

	rec = apiRequest(t, adaAPI, http.MethodGet, "/api/v1/documents/"+imp.DocumentID, "")
	doc := decodeJSON[struct {
		Title   string         `json:"title"`
		Content doccontent.Doc `json:"content"`
	}](t, rec)
	if rec.Code != http.StatusOK || doc.Title != "Guide" || len(doc.Content.Blocks) != 2 || doc.Content.Blocks[1].Text != "go run .\n" {
		t.Errorf("GET document = %d %+v", rec.Code, doc)
	}

	rec = apiRequest(t, adaAPI, http.MethodPost, "/api/v1/imports", `{"url":"`+pageURL+`#again"}`)
	again := decodeJSON[importResponse](t, rec)
	if rec.Code != http.StatusOK || again.Import.ID != imp.ID || again.Document["id"] != imp.DocumentID {
		t.Errorf("second POST = %d %+v, want 200 with the same import and document", rec.Code, again)
	}

	for _, target := range []string{"/api/v1/imports/" + imp.ID, "/api/v1/documents/" + imp.DocumentID} {
		if rec := apiRequest(t, graceAPI, http.MethodGet, target, ""); rec.Code != http.StatusNotFound {
			t.Errorf("another user's GET %s = %d, want 404", target, rec.Code)
		}
	}
	rec = apiRequest(t, graceAPI, http.MethodGet, "/api/v1/documents", "")
	if body := rec.Body.String(); rec.Code != http.StatusOK || body != "{\"documents\":[]}\n" {
		t.Errorf("another user's list = %d %s, want no documents", rec.Code, body)
	}
}

// TestDocumentPagesThroughAPI pages through 45 documents 20 at a time.
func TestDocumentPagesThroughAPI(t *testing.T) {
	db := pgtest.New(t)
	ctx := t.Context()
	st := store.New(db.Pool)
	ada, err := st.UpsertUserByEmail(ctx, "ada@example.com", "Ada")
	if err != nil {
		t.Fatal(err)
	}
	content := &ir.Doc{
		IRVersion: ir.Version, Title: "Page", Extractor: ir.Extractor{Name: "generic", Version: "2"},
		Source: ir.Source{URL: "https://docs.example.com/", FinalURL: "https://docs.example.com/", FetchedAt: t0, BodySHA256: fmt.Sprintf("%064d", 0)},
	}
	want := map[uuid.UUID]bool{}
	for i := range 45 {
		src, err := st.UpsertSource(ctx, store.Source{CanonicalURL: fmt.Sprintf("https://docs.example.com/p%d", i), FinalURL: "https://docs.example.com/", LastFetchedAt: t0})
		if err != nil {
			t.Fatal(err)
		}
		snap, err := st.SaveSnapshot(ctx, store.Snapshot{SourceID: src.ID, BodySHA256: fmt.Sprintf("%064d", i), Body: []byte("x"), ContentType: "text/html", FetchedAt: t0})
		if err != nil {
			t.Fatal(err)
		}
		ext, err := st.SaveExtraction(ctx, snap, content)
		if err != nil {
			t.Fatal(err)
		}
		id, _, err := st.CreateDocumentIfAbsent(ctx, store.NewDocument{OwnerID: ada.ID, SourceID: src.ID, ExtractionID: ext.ID, Content: ext.Doc})
		if err != nil {
			t.Fatal(err)
		}
		want[id] = true
	}
	h := New(Options{Logger: discardLogger, DB: db.Pool, Backend: integrationBackend{Store: st}, DevUser: ada.ID})

	var sizes []int
	seen := map[string]bool{}
	var lastCreated time.Time
	target := "/api/v1/documents?limit=20"
	for range 5 {
		rec := apiRequest(t, h, http.MethodGet, target, "")
		page := decodeJSON[struct {
			Documents []struct {
				ID        string    `json:"id"`
				CreatedAt time.Time `json:"created_at"`
			} `json:"documents"`
			NextCursor *string `json:"next_cursor"`
		}](t, rec)
		sizes = append(sizes, len(page.Documents))
		for _, d := range page.Documents {
			if seen[d.ID] {
				t.Errorf("document %s on two pages", d.ID)
			}
			if !lastCreated.IsZero() && d.CreatedAt.After(lastCreated) {
				t.Errorf("document %s is newer than the one before it", d.ID)
			}
			seen[d.ID], lastCreated = true, d.CreatedAt
		}
		if page.NextCursor == nil {
			break
		}
		target = "/api/v1/documents?limit=20&cursor=" + *page.NextCursor
	}
	if fmt.Sprint(sizes) != "[20 20 5]" || len(seen) != len(want) {
		t.Errorf("page sizes = %v covering %d documents, want [20 20 5] covering %d", sizes, len(seen), len(want))
	}
}
