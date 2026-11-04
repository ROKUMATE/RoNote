package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/httpapi/gen"
)

func resolveBody(t *testing.T, urls ...string) string {
	t.Helper()
	data, err := json.Marshal(map[string]any{"urls": urls})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestResolveDocuments(t *testing.T) {
	t.Parallel()
	guide := uuid.New()
	b := &fakeBackend{resolved: map[string]uuid.UUID{"https://docs.example.com/guide": guide}}
	h := New(Options{Logger: discardLogger, DB: dbUp, Backend: b, DevUser: devUser})

	rec := apiRequest(t, h, http.MethodPost, "/api/v1/documents/resolve", resolveBody(t,
		"https://Docs.Example.com/guide#install",
		"https://docs.example.com/other?utm_source=x",
		"ftp://docs.example.com/",
		"http://10.0.0.1/",
	))

	if rec.Code != http.StatusOK {
		t.Fatalf("POST = %d %s", rec.Code, rec.Body)
	}
	links := decodeJSON[gen.ResolvedLinks](t, rec).Links
	got := make([]string, len(links))
	for i, l := range links {
		got[i] = fmt.Sprintf("%s -> %v %v", l.URL, deref(l.CanonicalURL), deref(l.DocumentID))
	}
	want := []string{
		"https://Docs.Example.com/guide#install -> https://docs.example.com/guide " + guide.String(),
		"https://docs.example.com/other?utm_source=x -> https://docs.example.com/other <nil>",
		"ftp://docs.example.com/ -> <nil> <nil>",
		"http://10.0.0.1/ -> <nil> <nil>",
	}
	if !slices.Equal(got, want) {
		t.Errorf("links =\n%s\nwant\n%s", got, want)
	}
	if !slices.Equal(b.asked, []string{"https://docs.example.com/guide", "https://docs.example.com/other"}) || b.owners[0] != devUser {
		t.Errorf("backend asked %v for %v, want the canonical URLs for the dev user", b.asked, b.owners)
	}
}

func TestResolveDocumentsLimits(t *testing.T) {
	t.Parallel()
	h := New(Options{Logger: discardLogger, DB: dbUp, Backend: &fakeBackend{}, DevUser: devUser})
	many := make([]string, maxResolve+1)
	for i := range many {
		many[i] = fmt.Sprintf("https://docs.example.com/%d", i)
	}

	for name, body := range map[string]string{
		"no urls":    `{"urls":[]}`,
		"no body":    "",
		"too many":   resolveBody(t, many...),
		"not a list": `{"urls":"x"}`,
	} {
		rec := apiRequest(t, h, http.MethodPost, "/api/v1/documents/resolve", body)
		if rec.Code != http.StatusBadRequest || decodeError(t, rec).Code != gen.ErrorCodeInvalidRequest {
			t.Errorf("%s: POST = %d %s, want 400 invalid_request", name, rec.Code, rec.Body)
		}
	}
	if rec := apiRequest(t, h, http.MethodPost, "/api/v1/documents/resolve", resolveBody(t, many[:maxResolve]...)); rec.Code != http.StatusOK {
		t.Errorf("POST with %d urls = %d, want 200", maxResolve, rec.Code)
	}
}

// deref is *p, or nil, for messages.
func deref[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}
