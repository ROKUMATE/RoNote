//go:build integration

package importer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/rokumate/ronote/internal/blob"
	"github.com/rokumate/ronote/internal/docling"
	"github.com/rokumate/ronote/internal/ir"
	"github.com/rokumate/ronote/internal/jobs"
	"github.com/rokumate/ronote/internal/store"
)

// fakeConverter stands in for docling and pdftext. What a PDF gets is
// decided by a marker after its %PDF- header: ok, password, scan, pages301,
// broken, timeout or down.
type fakeConverter struct {
	t   *testing.T
	srv *httptest.Server

	mu    sync.Mutex
	calls []string          // "inspect <marker>", "convert <marker>", "code"
	tasks map[string]string // task id -> marker
}

func newFakeConverter(t *testing.T) *fakeConverter {
	f := &fakeConverter{t: t, tasks: map[string]string{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeConverter) client() *docling.Client {
	svc := docling.Service{URL: f.srv.URL, Key: "test-key"}
	return &docling.Client{Docling: svc, PDFText: svc, HTTP: f.srv.Client(), PollEvery: 5 * time.Millisecond}
}

func (f *fakeConverter) record(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}

// called returns the calls made so far.
func (f *fakeConverter) called() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// marker reads the test PDF's marker from an upload.
func marker(r *http.Request, field string) string {
	file, _, err := r.FormFile(field)
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }()
	data, _ := io.ReadAll(file)
	return strings.TrimSpace(strings.TrimPrefix(string(data), "%PDF-1.7 "))
}

// okDocument is docling's JSON for an ok PDF: a heading, a paragraph and a
// code block docling joined, over two pages.
const okDocument = `{
	"body": {"children": [{"$ref": "#/texts/0"}, {"$ref": "#/texts/1"}, {"$ref": "#/texts/2"}]},
	"texts": [
		{"self_ref": "#/texts/0", "label": "section_header", "level": 1, "text": "Functions", "prov": [{"page_no": 1, "bbox": {"l": 1, "t": 9, "r": 9, "b": 8, "coord_origin": "BOTTOMLEFT"}}]},
		{"self_ref": "#/texts/1", "label": "text", "text": "Deﬁne one:", "prov": [{"page_no": 1, "bbox": {"l": 1, "t": 7, "r": 9, "b": 6, "coord_origin": "BOTTOMLEFT"}}]},
		{"self_ref": "#/texts/2", "label": "code", "text": "def f(): return 1", "prov": [{"page_no": 2, "bbox": {"l": 48, "t": 590, "r": 323, "b": 448, "coord_origin": "BOTTOMLEFT"}}]}
	],
	"pages": {"1": {"page_no": 1}, "2": {"page_no": 2}}
}`

func (f *fakeConverter) serve(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Api-Key") != "test-key" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	reply := func(status int, body string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body) //nolint:gosec // a test's fixed JSON
	}
	switch {
	case r.URL.Path == "/v1/inspect":
		m := marker(r, "file")
		f.record("inspect " + m)
		switch m {
		case "password":
			reply(422, `{"error": "password"}`)
		case "broken":
			reply(422, `{"error": "unreadable"}`)
		case "scan":
			reply(200, `{"pages": 2, "chars": 0}`)
		case "pages301":
			reply(200, `{"pages": 301, "chars": 2300}`)
		case "down":
			reply(503, `{}`)
		default:
			reply(200, `{"pages": 2, "chars": 900}`)
		}
	case r.URL.Path == "/v1/convert/file/async":
		m := marker(r, "files")
		f.record("convert " + m)
		id := uuid.NewString()
		f.mu.Lock()
		f.tasks[id] = m
		f.mu.Unlock()
		reply(200, `{"task_id": "`+id+`", "task_status": "pending"}`)
	case strings.HasPrefix(r.URL.Path, "/v1/status/poll/"):
		reply(200, `{"task_id": "`+strings.TrimPrefix(r.URL.Path, "/v1/status/poll/")+`", "task_status": "success"}`)
	case strings.HasPrefix(r.URL.Path, "/v1/result/"):
		f.mu.Lock()
		m := f.tasks[strings.TrimPrefix(r.URL.Path, "/v1/result/")]
		f.mu.Unlock()
		if m == "timeout" {
			reply(200, `{"status": "partial_success", "document": {"json_content": null}, "errors": [{"error_message": "document timeout exceeded", "category": "timeout"}]}`)
			return
		}
		reply(200, `{"status": "success", "document": {"json_content": `+okDocument+`}, "errors": []}`)
	case r.URL.Path == "/v1/code":
		f.record("code")
		reply(200, `{"blocks": [["def f():", "    return 1"]]}`)
	default:
		f.t.Errorf("fake converter: unexpected %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func (h *harness) requestPDF(owner store.User, filename, body string) Requested {
	h.t.Helper()
	req, err := h.service.RequestPDF(h.t.Context(), owner.ID, filename, strings.NewReader(body), int64(len(body)))
	if err != nil {
		h.t.Fatalf("RequestPDF(%s): %v", filename, err)
	}
	return req
}

func TestPDFImport(t *testing.T) {
	h := newHarness(t, time.Hour)
	const pdf = "%PDF-1.7 ok"

	req := h.requestPDF(h.ada, "../Lecture\x00 3.pdf", pdf)
	if req.Existing || req.Import.Kind != store.ImportPDF || req.Import.Filename != "Lecture 3.pdf" {
		t.Fatalf("RequestPDF = %+v, want a new PDF import of Lecture 3.pdf", req)
	}
	imp := h.wait(h.ada, req.Import.ID)
	if imp.Status != store.ImportDone {
		t.Fatalf("import %s with %q, want done", imp.Status, imp.ErrorCode)
	}

	doc, err := h.store.GetDocument(t.Context(), h.ada.ID, imp.DocumentID)
	if err != nil {
		t.Fatal(err)
	}
	if doc.SourceURL != "" || doc.PDF == nil || *doc.PDF != (ir.PDF{Filename: "Lecture 3.pdf", Pages: 2}) {
		t.Errorf("document source %q, PDF %+v", doc.SourceURL, doc.PDF)
	}
	var code, prose ir.Block
	for _, b := range doc.Content.Blocks {
		switch b.Kind {
		case ir.KindCode:
			code = b.Block
		case ir.KindParagraph:
			prose = b.Block
		}
	}
	if code.Text != "def f():\n    return 1\n" || code.Page != 2 {
		t.Errorf("code block = %q on page %d, want the text layer's lines on page 2", code.Text, code.Page)
	}
	if prose.Content[0].Text != "Define one:" || prose.Page != 1 {
		t.Errorf("paragraph = %q on page %d", prose.Content[0].Text, prose.Page)
	}
	if want := []string{"inspect ok", "convert ok", "code"}; !slices.Equal(h.converter.called(), want) {
		t.Errorf("calls = %v, want %v", h.converter.called(), want)
	}
	if _, err := h.blobs.Get(t.Context(), PDFKey(h.ada.ID, sha(pdf))); err != nil {
		t.Errorf("the PDF is not in blob storage: %v", err)
	}

	t.Run("a redelivered job does nothing", func(t *testing.T) {
		before := len(h.converter.called())
		h.enqueuePDF(Args{ImportID: imp.ID, OwnerID: h.ada.ID})
		h.idle()
		if got := len(h.converter.called()); got != before {
			t.Errorf("a redelivered job called the converter %d more times", got-before)
		}
		if n := h.count("documents"); n != 1 {
			t.Errorf("%d documents, want 1", n)
		}
	})

	t.Run("the same file again is the same document", func(t *testing.T) {
		again := h.requestPDF(h.ada, "renamed.pdf", pdf)
		if !again.Existing || again.Import.DocumentID != imp.DocumentID {
			t.Errorf("second upload = %+v, want the existing document", again)
		}
	})

	t.Run("another user's copy is their own", func(t *testing.T) {
		theirs := h.wait(h.grace, h.requestPDF(h.grace, "mine.pdf", pdf).Import.ID)
		if theirs.Status != store.ImportDone || theirs.DocumentID == imp.DocumentID {
			t.Fatalf("grace's import: %+v", theirs)
		}
		var sources int
		if err := testDB.Pool.QueryRow(t.Context(), `SELECT count(*) FROM sources WHERE kind = 'pdf'`).Scan(&sources); err != nil {
			t.Fatal(err)
		}
		if sources != 2 {
			t.Errorf("%d PDF sources, want one per owner", sources)
		}
		if _, _, err := h.service.Original(t.Context(), h.grace.ID, imp.DocumentID); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("grace opening ada's original = %v, want ErrNotFound", err)
		}
		if _, err := h.blobs.Get(t.Context(), PDFKey(h.grace.ID, sha(pdf))); err != nil {
			t.Errorf("grace's PDF is not under her own key: %v", err)
		}
	})

	t.Run("the original is the uploaded file", func(t *testing.T) {
		name, obj, err := h.service.Original(t.Context(), h.ada.ID, imp.DocumentID)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = obj.Body.Close() }()
		data, _ := io.ReadAll(obj.Body)
		if name != "Lecture 3.pdf" || string(data) != pdf {
			t.Errorf("original = %q, %q", name, data)
		}
	})

	t.Run("deleting the document deletes its blob", func(t *testing.T) {
		if _, err := h.service.DeleteDocument(t.Context(), h.ada.ID, imp.DocumentID); err != nil {
			t.Fatal(err)
		}
		h.idle()
		if _, err := h.blobs.Get(t.Context(), PDFKey(h.ada.ID, sha(pdf))); !errors.Is(err, blob.ErrNotFound) {
			t.Errorf("after delete, Get = %v, want ErrNotFound", err)
		}
		var left int
		if err := testDB.Pool.QueryRow(t.Context(), `SELECT count(*) FROM sources WHERE kind = 'pdf' AND owner_id = $1`, h.ada.ID).Scan(&left); err != nil {
			t.Fatal(err)
		}
		if left != 0 {
			t.Errorf("%d PDF sources of ada's left", left)
		}
		if _, err := h.blobs.Get(t.Context(), PDFKey(h.grace.ID, sha(pdf))); err != nil {
			t.Errorf("deleting ada's document deleted grace's blob: %v", err)
		}
	})

	t.Run("deleting an account deletes its blobs", func(t *testing.T) {
		if err := h.service.DeleteUser(t.Context(), h.grace.ID); err != nil {
			t.Fatal(err)
		}
		h.idle()
		for info, err := range h.blobs.List(t.Context(), ownerPrefix(h.grace.ID)) {
			t.Errorf("blob %s left after deleting the account (%v)", info.Key, err)
		}
	})
}

func TestPDFImportFailures(t *testing.T) {
	tests := []struct {
		marker string
		code   string
		calls  []string
	}{
		{marker: "password", code: docling.CodePasswordProtected, calls: []string{"inspect password"}},
		{marker: "broken", code: docling.CodeFailed, calls: []string{"inspect broken"}},
		{marker: "scan", code: docling.CodeScanned, calls: []string{"inspect scan"}},
		{marker: "pages301", code: docling.CodeTooManyPages, calls: []string{"inspect pages301"}},
		// Unavailable is retried: every attempt calls again.
		{marker: "down", code: codeUnavailable, calls: []string{"inspect down", "inspect down", "inspect down"}},
		{marker: "timeout", code: codeUnavailable, calls: []string{"inspect timeout", "convert timeout", "inspect timeout", "convert timeout", "inspect timeout", "convert timeout"}},
	}
	for _, tt := range tests {
		t.Run(tt.marker, func(t *testing.T) {
			h := newHarness(t, time.Hour)
			imp := h.wait(h.ada, h.requestPDF(h.ada, tt.marker+".pdf", "%PDF-1.7 "+tt.marker).Import.ID)
			if imp.Status != store.ImportFailed || imp.ErrorCode != tt.code {
				t.Errorf("import %s with %q, want failed with %s", imp.Status, imp.ErrorCode, tt.code)
			}
			if got := h.converter.called(); !slices.Equal(got, tt.calls) {
				t.Errorf("calls = %v, want %v", got, tt.calls)
			}
		})
	}
}

func TestRequestPDFRefuses(t *testing.T) {
	h := newHarness(t, time.Hour)
	tests := []struct {
		name string
		body string
		size int64
		want error
	}{
		{name: "not a PDF", body: "<html>a page</html>", want: ErrNotPDF},
		{name: "too short to tell", body: "%PD", want: ErrNotPDF},
		{name: "over 25 MB", body: "%PDF-1.7", size: MaxPDFBytes + 1, want: ErrPDFTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			size := tt.size
			if size == 0 {
				size = int64(len(tt.body))
			}
			_, err := h.service.RequestPDF(t.Context(), h.ada.ID, "x.pdf", strings.NewReader(tt.body), size)
			if !errors.Is(err, tt.want) {
				t.Errorf("RequestPDF = %v, want %v", err, tt.want)
			}
		})
	}
	if n := h.count("imports"); n != 0 {
		t.Errorf("%d imports after refusals, want none", n)
	}
	for info, err := range h.blobs.List(t.Context(), "") {
		t.Errorf("blob %s stored for a refused upload (%v)", info.Key, err)
	}
}

func TestPDFImportsCountTowardTheQuota(t *testing.T) {
	h := newHarness(t, time.Hour)
	svc := NewService(h.store, h.queue, Quota{ImportsPerDay: 1, Documents: 10}, h.blobs)
	if _, err := svc.Request(t.Context(), h.ada.ID, h.site.url("docs.test", "/a")); err != nil {
		t.Fatal(err)
	}
	_, err := svc.RequestPDF(t.Context(), h.ada.ID, "a.pdf", strings.NewReader("%PDF-1.7 ok"), 11)
	var over *QuotaError
	if !errors.As(err, &over) {
		t.Errorf("a PDF over the daily quota = %v, want a QuotaError", err)
	}
}

func TestBlobSweep(t *testing.T) {
	h := newHarness(t, time.Hour)
	ctx := t.Context()
	done := h.wait(h.ada, h.requestPDF(h.ada, "kept.pdf", "%PDF-1.7 ok").Import.ID)
	if done.Status != store.ImportDone {
		t.Fatalf("import %s", done.Status)
	}
	kept := PDFKey(h.ada.ID, sha("%PDF-1.7 ok"))
	put := func(key string) {
		t.Helper()
		if err := h.blobs.Put(ctx, key, strings.NewReader("%PDF-"), 5); err != nil {
			t.Fatal(err)
		}
	}
	orphan := PDFKey(h.ada.ID, sha("never imported"))
	gone := PDFKey(uuid.New(), sha("a deleted account's"))
	put(orphan)
	put(gone)

	// Fresh blobs stay: an upload's blob lands just before its import.
	if n, err := h.cleaner.Sweep(ctx); err != nil || n != 0 {
		t.Fatalf("Sweep of fresh blobs = %d, %v; want 0", n, err)
	}
	h.blobs.Age(2 * sweepAge)
	for range 2 {
		if _, err := h.cleaner.Sweep(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for _, key := range []string{orphan, gone} {
		if _, err := h.blobs.Get(ctx, key); !errors.Is(err, blob.ErrNotFound) {
			t.Errorf("leftover %s still there: %v", key, err)
		}
	}
	if _, err := h.blobs.Get(ctx, kept); err != nil {
		t.Errorf("the sweep deleted a document's PDF: %v", err)
	}
}

// enqueuePDF enqueues a PDF import's job again, without its key, as a job
// delivered twice would run.
func (h *harness) enqueuePDF(args Args) {
	h.t.Helper()
	payload, err := json.Marshal(args)
	if err != nil {
		h.t.Fatal(err)
	}
	err = h.store.InTx(h.t.Context(), func(_ *store.Store, raw pgx.Tx) error {
		return h.queue.EnqueueTx(h.t.Context(), raw, jobs.Job{Kind: PDFKind, Payload: payload})
	})
	if err != nil {
		h.t.Fatal(err)
	}
}

// idle waits until River has no job left to run.
func (h *harness) idle() {
	h.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		var n int
		if err := testDB.Pool.QueryRow(h.t.Context(), `SELECT count(*) FROM river_job WHERE state NOT IN ('completed', 'cancelled', 'discarded')`).Scan(&n); err != nil {
			h.t.Fatal(err)
		}
		if n == 0 {
			return
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("%d jobs still to run after 20s", n)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// sha is the hex SHA-256 of s.
func sha(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
