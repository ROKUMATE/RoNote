package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rokumate/ronote/internal/httpapi/gen"
)

func TestCrossOriginRequests(t *testing.T) {
	t.Parallel()
	const resolve = `{"urls":["https://docs.example.com/guide"]}`
	tests := []struct {
		name    string
		method  string
		headers map[string]string
		want    int
	}{
		{name: "cross-site POST, by Sec-Fetch-Site", method: http.MethodPost, headers: map[string]string{"Sec-Fetch-Site": "cross-site"}, want: http.StatusForbidden},
		{name: "same-site POST from a sibling subdomain, by Sec-Fetch-Site", method: http.MethodPost, headers: map[string]string{"Sec-Fetch-Site": "same-site"}, want: http.StatusForbidden},
		{name: "cross-site POST, by Origin alone", method: http.MethodPost, headers: map[string]string{"Origin": "https://evil.test"}, want: http.StatusForbidden},
		{name: "same-origin POST", method: http.MethodPost, headers: map[string]string{"Sec-Fetch-Site": "same-origin"}, want: http.StatusOK},
		{name: "POST from the public URL", method: http.MethodPost, headers: map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://ronote.test"}, want: http.StatusOK},
		{name: "POST with neither header, not from a browser", method: http.MethodPost, want: http.StatusOK},
		{name: "cross-site GET", method: http.MethodGet, headers: map[string]string{"Sec-Fetch-Site": "cross-site"}, want: http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := newRequest(t, tt.method, "/api/v1/documents")
			if tt.method == http.MethodPost {
				req = httptest.NewRequestWithContext(t.Context(), tt.method, "/api/v1/documents/resolve", strings.NewReader(resolve))
				req.Header.Set("Content-Type", "application/json")
			}
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			h := signedIn(Options{Logger: discardLogger, DB: dbUp, Backend: &fakeBackend{}, PublicURL: "https://ronote.test"})

			rec := record(h, req)

			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.want, rec.Body)
			}
			if tt.want == http.StatusForbidden {
				checkContract(t, req, rec)
				if got := decodeError(t, rec); got.Code != gen.ErrorCodeCrossOrigin {
					t.Errorf("code = %q, want cross_origin", got.Code)
				}
			}
		})
	}
}
