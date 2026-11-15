package httpapi

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/importer"
	"github.com/rokumate/ronote/internal/store"
)

// upload is a multipart request with the given file parts, each named
// file unless the name says otherwise, as name=filename.
func upload(t *testing.T, h http.Handler, files map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for spec, data := range files {
		field, filename, ok := strings.Cut(spec, "=")
		if !ok {
			field, filename = "file", spec
		}
		part, err := w.CreateFormFile(field, filename)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write([]byte(data))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/imports/pdf", &body)
	r.Header.Set("Content-Type", w.FormDataContentType())
	rec := record(h, r)
	checkContract(t, r, rec)
	return rec
}

func TestCreatePdfImport(t *testing.T) {
	t.Parallel()
	queued := store.Import{ID: uuid.New(), OwnerID: testUser, Kind: store.ImportPDF, Filename: "Lecture 3.pdf", Status: store.ImportQueued, CreatedAt: t0, UpdatedAt: t0}
	var got struct {
		filename string
		data     string
	}
	b := &fakeBackend{requestPDF: func(filename string, data []byte) (importer.Requested, error) {
		got.filename, got.data = filename, string(data)
		switch {
		case !strings.HasPrefix(string(data), "%PDF-"):
			return importer.Requested{}, importer.ErrNotPDF
		case string(data) == "%PDF-quota":
			return importer.Requested{}, &importer.QuotaError{Quota: "imports per day"}
		}
		return importer.Requested{Import: queued}, nil
	}}
	h := signedIn(Options{Logger: discardLogger, DB: dbUp, Backend: b})

	rec := upload(t, h, map[string]string{"Lecture 3.pdf": "%PDF-1.7 body"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("upload = %d %s, want 202", rec.Code, rec.Body)
	}
	imp := decodeJSON[struct {
		Import struct {
			Kind, Filename, Status string
			RequestedURL           *string `json:"requested_url"`
		} `json:"import"`
	}](t, rec).Import
	if imp.Kind != "pdf" || imp.Filename != "Lecture 3.pdf" || imp.Status != "queued" || imp.RequestedURL != nil {
		t.Errorf("import = %+v", imp)
	}
	if got.filename != "Lecture 3.pdf" || got.data != "%PDF-1.7 body" {
		t.Errorf("backend got %q, %q", got.filename, got.data)
	}

	tests := []struct {
		name   string
		files  map[string]string
		status int
		code   string
	}{
		{name: "not a PDF", files: map[string]string{"a.pdf": "<html>"}, status: http.StatusBadRequest, code: "not_a_pdf"},
		{name: "no file part", files: map[string]string{"other=a.pdf": "%PDF-"}, status: http.StatusBadRequest, code: "invalid_request"},
		{name: "two files", files: map[string]string{"a.pdf": "%PDF-", "file=b.pdf": "%PDF-"}, status: http.StatusBadRequest, code: "invalid_request"},
		{name: "over the quota", files: map[string]string{"a.pdf": "%PDF-quota"}, status: http.StatusTooManyRequests, code: "quota_exceeded"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := upload(t, h, tt.files)
			if rec.Code != tt.status || string(decodeError(t, rec).Code) != tt.code {
				t.Errorf("upload = %d %s, want %d %s", rec.Code, rec.Body, tt.status, tt.code)
			}
		})
	}
}

func TestCreatePdfImportTooLarge(t *testing.T) {
	t.Parallel()
	h := signedIn(Options{Logger: discardLogger, DB: dbUp, Backend: &fakeBackend{}})

	t.Run("says so up front", func(t *testing.T) {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/imports/pdf", strings.NewReader("x"))
		r.Header.Set("Content-Type", "multipart/form-data; boundary=x")
		r.ContentLength = maxPDFBody + 1
		rec := record(h, r)
		if rec.Code != http.StatusRequestEntityTooLarge || decodeError(t, rec).Code != "pdf_too_large" {
			t.Errorf("upload = %d %s, want 413 pdf_too_large", rec.Code, rec.Body)
		}
	})
	t.Run("found while reading", func(t *testing.T) {
		big := "%PDF-" + strings.Repeat("x", importer.MaxPDFBytes+64<<10)
		rec := upload(t, h, map[string]string{"big.pdf": big})
		if rec.Code != http.StatusRequestEntityTooLarge || decodeError(t, rec).Code != "pdf_too_large" {
			t.Errorf("upload = %d %.200s, want 413 pdf_too_large", rec.Code, rec.Body)
		}
	})
}

func TestGetDocumentOriginal(t *testing.T) {
	t.Parallel()
	doc, other := uuid.New(), uuid.New()
	b := &fakeBackend{originals: []original{
		{id: doc, owner: testUser, filename: `Week "3"; notes\..\é.pdf`, data: "%PDF-1.7 bytes"},
		{id: other, owner: uuid.New(), filename: "theirs.pdf", data: "%PDF-1.7 theirs"},
	}}
	h := signedIn(Options{Logger: discardLogger, DB: dbUp, Backend: b})

	rec := apiRequest(t, h, http.MethodGet, "/api/v1/documents/"+doc.String()+"/original", "")
	if rec.Code != http.StatusOK || rec.Body.String() != "%PDF-1.7 bytes" {
		t.Fatalf("original = %d %q", rec.Code, rec.Body)
	}
	want := map[string]string{
		"Content-Type":            "application/pdf",
		"X-Content-Type-Options":  "nosniff",
		"Content-Security-Policy": "sandbox; default-src 'none'",
		"Content-Disposition":     `inline; filename="Week 3 notes.._.pdf"; filename*=UTF-8''Week%203%20notes..%C3%A9.pdf`,
		"Content-Length":          "14",
	}
	for name, value := range want {
		if got := rec.Header().Get(name); got != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}

	for name, id := range map[string]uuid.UUID{"another user's": other, "missing": uuid.New()} {
		rec := apiRequest(t, h, http.MethodGet, "/api/v1/documents/"+id.String()+"/original", "")
		if rec.Code != http.StatusNotFound || rec.Header().Get("Content-Type") == "application/pdf" {
			t.Errorf("%s original = %d %s, want 404", name, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
}

func TestContentDisposition(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"Lecture 3.pdf":          `inline; filename="Lecture 3.pdf"; filename*=UTF-8''Lecture%203.pdf`,
		"notes":                  `inline; filename="notes.pdf"; filename*=UTF-8''notes.pdf`,
		"":                       `inline; filename="document.pdf"; filename*=UTF-8''document.pdf`,
		"..":                     `inline; filename="document.pdf"; filename*=UTF-8''document.pdf`,
		"a\r\nSet-Cookie: x.pdf": `inline; filename="aSet-Cookie: x.pdf"; filename*=UTF-8''aSet-Cookie%3A%20x.pdf`,
		"100%.PDF":               `inline; filename="100_.PDF"; filename*=UTF-8''100%25.PDF`,
		"日本語.pdf":                `inline; filename="___.pdf"; filename*=UTF-8''%E6%97%A5%E6%9C%AC%E8%AA%9E.pdf`,
	}
	for in, want := range tests {
		if got := contentDisposition(in); got != want {
			t.Errorf("contentDisposition(%q) = %s, want %s", in, got, want)
		}
	}
}
