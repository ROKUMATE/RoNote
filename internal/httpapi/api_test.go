package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/httpapi/gen"
	"github.com/rokumate/ronote/internal/importer"
	"github.com/rokumate/ronote/internal/ir"
	"github.com/rokumate/ronote/internal/store"
)

var (
	devUser = uuid.MustParse("0192f1c4-0000-7000-8000-000000000001")
	t0      = time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)
)

// fakeBackend answers from its fields and records the owner it was asked for.
type fakeBackend struct {
	request func(url string) (importer.Requested, error)
	imports map[uuid.UUID]store.Import
	docs    []store.Document // newest first
	owners  []uuid.UUID
	after   *store.DocumentCursor
	fail    error
}

func (f *fakeBackend) Request(_ context.Context, owner uuid.UUID, url string) (importer.Requested, error) {
	f.owners = append(f.owners, owner)
	return f.request(url)
}

func (f *fakeBackend) GetImport(_ context.Context, owner, id uuid.UUID) (store.Import, error) {
	f.owners = append(f.owners, owner)
	if f.fail != nil {
		return store.Import{}, f.fail
	}
	if imp, ok := f.imports[id]; ok {
		return imp, nil
	}
	return store.Import{}, store.ErrNotFound
}

func (f *fakeBackend) GetDocument(_ context.Context, owner, id uuid.UUID) (store.Document, error) {
	f.owners = append(f.owners, owner)
	for _, d := range f.docs {
		if d.ID == id {
			return d, nil
		}
	}
	return store.Document{}, store.ErrNotFound
}

func (f *fakeBackend) ListDocuments(_ context.Context, owner uuid.UUID, after *store.DocumentCursor, limit int) ([]store.DocumentSummary, error) {
	f.owners = append(f.owners, owner)
	f.after = after
	var out []store.DocumentSummary
	for _, d := range f.docs {
		if after != nil && !d.CreatedAt.Before(after.CreatedAt) {
			continue
		}
		if len(out) < limit {
			out = append(out, d.DocumentSummary)
		}
	}
	return out, nil
}

func testDoc(i int) store.Document {
	content := &ir.Doc{
		IRVersion: ir.Version, Title: fmt.Sprintf("Doc %d", i), Extractor: ir.Extractor{Name: "generic", Version: "2"},
		Source: ir.Source{URL: "https://docs.example.com/", FinalURL: "https://docs.example.com/", FetchedAt: t0, BodySHA256: strings.Repeat("a", 64)},
		Blocks: []ir.Block{{ID: "a1", Kind: ir.KindCode, Text: "x := 1\n"}},
	}
	return store.Document{
		DocumentSummary: store.DocumentSummary{
			ID: uuid.New(), Title: content.Title, SourceURL: fmt.Sprintf("https://docs.example.com/%d", i), Version: 1,
			CreatedAt: t0.Add(time.Duration(-i) * time.Minute), UpdatedAt: t0,
		},
		Content: content,
	}
}

func apiRequest(t *testing.T, h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequestWithContext(t.Context(), method, target, http.NoBody)
	} else {
		r = httptest.NewRequestWithContext(t.Context(), method, target, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	rec := record(h, r)
	checkContract(t, r, rec)
	return rec
}

func decodeJSON[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("body %q: %v", rec.Body, err)
	}
	return v
}

func TestAPIRequiresAUser(t *testing.T) {
	t.Parallel()
	h := New(Options{Logger: discardLogger, DB: dbUp, Backend: &fakeBackend{}})
	for _, target := range []string{"/api/v1/documents", "/api/v1/imports/not-a-uuid", "/api/v1/nope"} {
		rec := apiRequest(t, h, http.MethodGet, target, "")
		if rec.Code != http.StatusUnauthorized || decodeError(t, rec).Code != "unauthenticated" {
			t.Errorf("GET %s = %d %s, want 401 unauthenticated", target, rec.Code, rec.Body)
		}
	}
	if rec := apiRequest(t, h, http.MethodGet, "/healthz", ""); rec.Code != http.StatusOK {
		t.Errorf("/healthz = %d, want it open", rec.Code)
	}
}

func TestCreateImport(t *testing.T) {
	t.Parallel()
	queued := store.Import{ID: uuid.New(), RequestedURL: "https://go.dev/doc#x", CanonicalURL: "https://go.dev/doc", Status: store.ImportQueued, CreatedAt: t0, UpdatedAt: t0}
	doc := testDoc(1)
	done := queued
	done.Status, done.DocumentID = store.ImportDone, doc.ID

	tests := []struct {
		name       string
		body       string
		result     importer.Requested
		err        error
		wantStatus int
		wantCode   gen.ErrorCode
	}{
		{name: "queued", body: `{"url":"https://go.dev/doc#x"}`, result: importer.Requested{Import: queued}, wantStatus: http.StatusAccepted},
		{name: "already imported", body: `{"url":"https://go.dev/doc"}`, result: importer.Requested{Import: done, Existing: true}, wantStatus: http.StatusOK},
		{name: "not JSON", body: `{"url":`, wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{name: "no body", wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{name: "invalid URL", body: `{"url":"go.dev"}`, err: importer.ErrInvalidURL, wantStatus: http.StatusBadRequest, wantCode: "invalid_url"},
		{name: "scheme", body: `{"url":"ftp://go.dev"}`, err: importer.ErrUnsupportedScheme, wantStatus: http.StatusBadRequest, wantCode: "unsupported_scheme"},
		{name: "too long", body: `{"url":"https://x"}`, err: importer.ErrURLTooLong, wantStatus: http.StatusBadRequest, wantCode: "url_too_long"},
		{name: "IP literal", body: `{"url":"http://10.0.0.1/"}`, err: importer.ErrIPLiteralHost, wantStatus: http.StatusBadRequest, wantCode: "ip_literal_host"},
		{name: "backend down", body: `{"url":"https://go.dev/"}`, err: errors.New("db down"), wantStatus: http.StatusInternalServerError, wantCode: "internal_error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := &fakeBackend{docs: []store.Document{doc}, request: func(string) (importer.Requested, error) { return tt.result, tt.err }}
			h := New(Options{Logger: discardLogger, DB: dbUp, Backend: b, DevUser: devUser})

			rec := apiRequest(t, h, http.MethodPost, "/api/v1/imports", tt.body)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d %s, want %d", rec.Code, rec.Body, tt.wantStatus)
			}
			if tt.wantCode != "" {
				if got := decodeError(t, rec); got.Code != tt.wantCode || got.RequestID == "" {
					t.Errorf("error = %+v, want code %s with a request ID", got, tt.wantCode)
				}
				return
			}
			got := decodeJSON[struct {
				Import   map[string]any `json:"import"`
				Document map[string]any `json:"document"`
			}](t, rec)
			if got.Import["id"] != tt.result.Import.ID.String() || got.Import["status"] != string(tt.result.Import.Status) {
				t.Errorf("import = %v", got.Import)
			}
			if tt.result.Existing != (got.Document != nil) || (got.Document != nil && got.Document["title"] != "Doc 1") {
				t.Errorf("document = %v, want it only when the user already had one", got.Document)
			}
			if len(b.owners) == 0 || b.owners[0] != devUser {
				t.Errorf("backend asked for owners %v, want the dev user", b.owners)
			}
		})
	}
}

func TestGetImportAndDocument(t *testing.T) {
	t.Parallel()
	doc := testDoc(1)
	imp := store.Import{ID: uuid.New(), Status: store.ImportFailed, ErrorCode: "not_found", CreatedAt: t0, UpdatedAt: t0}
	b := &fakeBackend{imports: map[uuid.UUID]store.Import{imp.ID: imp}, docs: []store.Document{doc}}
	h := New(Options{Logger: discardLogger, DB: dbUp, Backend: b, DevUser: devUser})

	rec := apiRequest(t, h, http.MethodGet, "/api/v1/imports/"+imp.ID.String(), "")
	if got := decodeJSON[map[string]map[string]any](t, rec)["import"]; rec.Code != http.StatusOK || got["error_code"] != "not_found" || got["document_id"] != nil {
		t.Errorf("GET import = %d %v", rec.Code, got)
	}
	rec = apiRequest(t, h, http.MethodGet, "/api/v1/documents/"+doc.ID.String(), "")
	got := decodeJSON[struct {
		Title   string `json:"title"`
		Version int    `json:"version"`
		Content ir.Doc `json:"content"`
	}](t, rec)
	if rec.Code != http.StatusOK || got.Title != "Doc 1" || got.Version != 1 || got.Content.Blocks[0].Text != "x := 1\n" {
		t.Errorf("GET document = %d %+v", rec.Code, got)
	}

	for _, target := range []string{"/api/v1/imports/" + uuid.NewString(), "/api/v1/documents/" + uuid.NewString()} {
		if rec := apiRequest(t, h, http.MethodGet, target, ""); rec.Code != http.StatusNotFound || decodeError(t, rec).Code != "not_found" {
			t.Errorf("GET %s = %d %s, want 404 not_found", target, rec.Code, rec.Body)
		}
	}
	for _, target := range []string{"/api/v1/imports/12", "/api/v1/documents/not-a-uuid"} {
		if rec := apiRequest(t, h, http.MethodGet, target, ""); rec.Code != http.StatusBadRequest || decodeError(t, rec).Code != "invalid_parameter" {
			t.Errorf("GET %s = %d %s, want 400 invalid_parameter", target, rec.Code, rec.Body)
		}
	}
	if rec := apiRequest(t, h, http.MethodGet, "/api/v1/nope", ""); rec.Code != http.StatusNotFound || decodeError(t, rec).Code != "not_found" {
		t.Errorf("unknown API path = %d %s, want the 404 envelope", rec.Code, rec.Body)
	}
	if rec := apiRequest(t, h, http.MethodDelete, "/api/v1/documents/"+doc.ID.String(), ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE document = %d, want 405", rec.Code)
	}
	b.fail = errors.New("db down")
	if rec := apiRequest(t, h, http.MethodGet, "/api/v1/imports/"+imp.ID.String(), ""); rec.Code != http.StatusInternalServerError || decodeError(t, rec).Code != "internal_error" {
		t.Errorf("GET import with the backend down = %d %s, want 500 internal_error", rec.Code, rec.Body)
	}
}

func TestListDocuments(t *testing.T) {
	t.Parallel()
	b := &fakeBackend{}
	for i := range 5 {
		b.docs = append(b.docs, testDoc(i))
	}
	h := New(Options{Logger: discardLogger, DB: dbUp, Backend: b, DevUser: devUser})

	type list struct {
		Documents []struct {
			ID string `json:"id"`
		} `json:"documents"`
		NextCursor *string `json:"next_cursor"`
	}
	var seen []string
	target := "/api/v1/documents?limit=2"
	for range 4 {
		rec := apiRequest(t, h, http.MethodGet, target, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d %s", target, rec.Code, rec.Body)
		}
		page := decodeJSON[list](t, rec)
		for _, d := range page.Documents {
			seen = append(seen, d.ID)
		}
		if page.NextCursor == nil {
			break
		}
		target = "/api/v1/documents?limit=2&cursor=" + *page.NextCursor
	}
	var want []string
	for _, d := range b.docs {
		want = append(want, d.ID.String())
	}
	if fmt.Sprint(seen) != fmt.Sprint(want) {
		t.Errorf("pages = %v, want %v", seen, want)
	}

	if rec := apiRequest(t, h, http.MethodGet, "/api/v1/documents", ""); len(decodeJSON[list](t, rec).Documents) != 5 {
		t.Errorf("default page = %s, want all 5 (limit 20)", rec.Body)
	}
	for target, code := range map[string]gen.ErrorCode{
		"/api/v1/documents?limit=0":         "invalid_parameter",
		"/api/v1/documents?limit=101":       "invalid_parameter",
		"/api/v1/documents?limit=ten":       "invalid_parameter",
		"/api/v1/documents?cursor=%21%21":   "invalid_cursor",
		"/api/v1/documents?cursor=bm9waXBl": "invalid_cursor",
	} {
		if rec := apiRequest(t, h, http.MethodGet, target, ""); rec.Code != http.StatusBadRequest || decodeError(t, rec).Code != code {
			t.Errorf("GET %s = %d %s, want 400 %s", target, rec.Code, rec.Body, code)
		}
	}
}

func TestCursorRoundTrip(t *testing.T) {
	t.Parallel()
	c := store.DocumentCursor{CreatedAt: time.Date(2026, 11, 4, 9, 0, 0, 123456789, time.UTC), ID: uuid.New()}
	got, err := decodeCursor(encodeCursor(c))
	if err != nil || !got.CreatedAt.Equal(c.CreatedAt) || got.ID != c.ID {
		t.Errorf("round trip = %+v, %v; want %+v", got, err, c)
	}
}

// TestTimestampsAreUTC requires every timestamp in a response to be RFC 3339
// in UTC, whatever zone the database driver returned it in.
func TestTimestampsAreUTC(t *testing.T) {
	t.Parallel()
	ist := time.FixedZone("IST", 5*3600+1800)
	doc := testDoc(1)
	doc.CreatedAt, doc.UpdatedAt = doc.CreatedAt.In(ist), doc.UpdatedAt.In(ist)
	imp := store.Import{ID: uuid.New(), Status: store.ImportDone, DocumentID: doc.ID, CreatedAt: t0.In(ist), UpdatedAt: t0.In(ist)}
	existing := true
	b := &fakeBackend{
		imports: map[uuid.UUID]store.Import{imp.ID: imp},
		docs:    []store.Document{doc},
		request: func(string) (importer.Requested, error) {
			return importer.Requested{Import: imp, Existing: existing}, nil
		},
	}
	h := New(Options{Logger: discardLogger, DB: dbUp, Backend: b, DevUser: devUser})

	requests := []struct{ method, target, body string }{
		{http.MethodPost, "/api/v1/imports", `{"url":"https://docs.example.com/1"}`},
		{http.MethodGet, "/api/v1/imports/" + imp.ID.String(), ""},
		{http.MethodGet, "/api/v1/documents", ""},
		{http.MethodGet, "/api/v1/documents/" + doc.ID.String(), ""},
	}
	check := func(name string, body any) {
		found := 0
		walkTimestamps(body, func(key, value string) {
			found++
			got, err := time.Parse(time.RFC3339Nano, value)
			if err != nil || !strings.HasSuffix(value, "Z") || !got.Equal(t0) && !got.Equal(doc.CreatedAt) {
				t.Errorf("%s: %s = %q, want an RFC 3339 UTC time", name, key, value)
			}
		})
		if found == 0 {
			t.Errorf("%s: no timestamps found", name)
		}
	}
	for _, r := range requests {
		rec := apiRequest(t, h, r.method, r.target, r.body)
		check(r.method+" "+r.target, decodeJSON[any](t, rec))
	}
	existing = false
	rec := apiRequest(t, h, http.MethodPost, "/api/v1/imports", `{"url":"https://docs.example.com/1"}`)
	check("POST /api/v1/imports (queued)", decodeJSON[any](t, rec))
}

// walkTimestamps calls f for every string field whose key ends in _at.
func walkTimestamps(v any, f func(key, value string)) {
	switch v := v.(type) {
	case map[string]any:
		for k, child := range v {
			if s, ok := child.(string); ok && strings.HasSuffix(k, "_at") {
				f(k, s)
				continue
			}
			walkTimestamps(child, f)
		}
	case []any:
		for _, child := range v {
			walkTimestamps(child, f)
		}
	}
}
