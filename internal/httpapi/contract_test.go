package httpapi

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/legacy"
	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/importer"
	"github.com/rokumate/ronote/internal/store"
)

// loadContract reads api/openapi.yaml once for every test in the package.
var loadContract = sync.OnceValues(func() (*openapi3.T, error) {
	loader := openapi3.NewLoader()
	spec, err := loader.LoadFromFile(filepath.Join("..", "..", "api", "openapi.yaml"))
	if err != nil {
		return nil, err
	}
	// The spec's servers entry is the path prefix every route lives under.
	spec.Servers = openapi3.Servers{{URL: "http://example.com/api/v1"}}
	return spec, nil
})

var contractRouter = sync.OnceValues(func() (routers.Router, error) {
	spec, err := loadContract()
	if err != nil {
		return nil, err
	}
	return legacy.NewRouter(spec)
})

// checkContract fails the test when rec, the answer to r, is not a response
// the spec allows for that operation: an undeclared status, or a body that
// does not match its schema. Requests to routes the spec does not have are
// left alone; tests send those on purpose. Event streams are not JSON and
// are checked by their own tests.
func checkContract(t *testing.T, r *http.Request, rec *httptest.ResponseRecorder) {
	t.Helper()
	router, err := contractRouter()
	if err != nil {
		t.Fatal(err)
	}
	u := *r.URL
	u.Scheme, u.Host = "http", "example.com"
	probe := r.Clone(r.Context())
	probe.URL, probe.Host, probe.Body = &u, "example.com", http.NoBody
	route, params, err := router.FindRoute(probe)
	// The legacy router makes a new RouteError each time, so compare reasons.
	if re := (*routers.RouteError)(nil); errors.As(err, &re) && (re.Reason == routers.ErrPathNotFound.Error() || re.Reason == routers.ErrMethodNotAllowed.Error()) {
		return
	}
	if err != nil {
		t.Fatalf("%s %s: %v", r.Method, r.URL, err)
	}
	if strings.HasPrefix(rec.Header().Get("Content-Type"), "text/event-stream") {
		return
	}
	err = openapi3filter.ValidateResponse(r.Context(), &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{Request: probe, PathParams: params, Route: route},
		Status:                 rec.Code,
		Header:                 rec.Header(),
		Body:                   io.NopCloser(bytes.NewReader(rec.Body.Bytes())),
		Options: &openapi3filter.Options{
			IncludeResponseStatus:   true,
			SchemaValidationOptions: []openapi3.SchemaValidationOption{openapi3.EnableFormatValidation()},
		},
	})
	if err != nil {
		t.Errorf("%s %s answered %d, which the spec does not allow: %v\nbody: %s", r.Method, r.URL, rec.Code, err, rec.Body)
	}
}

// TestRepeatImportReturnsTheFullImport requires both answers to a POST, the
// 202 for a new import and the 200 for a page the user already has, to carry
// the whole Import object.
func TestRepeatImportReturnsTheFullImport(t *testing.T) {
	t.Parallel()
	spec, err := loadContract()
	if err != nil {
		t.Fatal(err)
	}
	required := spec.Components.Schemas["Import"].Value.Required
	doc := testDoc(1)
	queued := store.Import{ID: uuid.New(), RequestedURL: "https://docs.example.com/1", CanonicalURL: "https://docs.example.com/1", Status: store.ImportQueued, CreatedAt: t0, UpdatedAt: t0}
	done := queued
	done.Status, done.DocumentID = store.ImportDone, doc.ID

	for _, tt := range []struct {
		name   string
		result importer.Requested
		want   []string
	}{
		{name: "new", result: importer.Requested{Import: queued}, want: required},
		{name: "repeat", result: importer.Requested{Import: done, Existing: true}, want: append(slices.Clone(required), "document_id")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := &fakeBackend{docs: []store.Document{doc}, request: func(string) (importer.Requested, error) { return tt.result, nil }}
			h := signedIn(Options{Logger: discardLogger, DB: dbUp, Backend: b})

			got := decodeJSON[map[string]map[string]any](t, apiRequest(t, h, http.MethodPost, "/api/v1/imports", `{"url":"https://docs.example.com/1"}`))["import"]
			for _, field := range tt.want {
				if _, ok := got[field]; !ok {
					t.Errorf("import has no %s: %v", field, got)
				}
			}
		})
	}
}
