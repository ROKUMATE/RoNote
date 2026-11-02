package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

var discardLogger = slog.New(slog.DiscardHandler)

func TestRoutes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		method     string
		target     string
		wantStatus int
		wantBody   string
		wantCode   string
		wantAllow  string
	}{
		{name: "healthz", method: http.MethodGet, target: "/healthz", wantStatus: http.StatusOK, wantBody: `{"status":"ok"}` + "\n"},
		{name: "readyz", method: http.MethodGet, target: "/readyz", wantStatus: http.StatusOK, wantBody: `{"status":"ok"}` + "\n"},
		{name: "unknown path", method: http.MethodGet, target: "/nope", wantStatus: http.StatusNotFound, wantCode: "not_found"},
		{
			name: "wrong method", method: http.MethodPost, target: "/healthz",
			wantStatus: http.StatusMethodNotAllowed, wantCode: "method_not_allowed", wantAllow: "GET, HEAD",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := record(New(discardLogger), newRequest(t, tt.method, tt.target))

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", got)
			}
			if got := rec.Header().Get("Allow"); got != tt.wantAllow {
				t.Errorf("Allow = %q, want %q", got, tt.wantAllow)
			}
			if tt.wantCode == "" {
				if got := rec.Body.String(); got != tt.wantBody {
					t.Errorf("body = %q, want %q", got, tt.wantBody)
				}
				return
			}
			got := decodeError(t, rec)
			if got.Code != tt.wantCode {
				t.Errorf("error code = %q, want %q", got.Code, tt.wantCode)
			}
			if want := rec.Header().Get(requestIDHeader); got.RequestID != want {
				t.Errorf("error request_id = %q, want the X-Request-ID header %q", got.RequestID, want)
			}
		})
	}
}

func TestNewServerSetsLimits(t *testing.T) {
	t.Parallel()

	srv := NewServer(New(discardLogger), discardLogger)

	limits := map[string]int64{
		"ReadHeaderTimeout": int64(srv.ReadHeaderTimeout),
		"ReadTimeout":       int64(srv.ReadTimeout),
		"WriteTimeout":      int64(srv.WriteTimeout),
		"IdleTimeout":       int64(srv.IdleTimeout),
		"MaxHeaderBytes":    int64(srv.MaxHeaderBytes),
	}
	for name, v := range limits {
		if v <= 0 {
			t.Errorf("%s = %d, want > 0", name, v)
		}
	}
}

func newRequest(t *testing.T, method, target string) *http.Request {
	t.Helper()
	return httptest.NewRequestWithContext(t.Context(), method, target, http.NoBody)
}

func record(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) errorBody {
	t.Helper()
	var env errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("body %q is not an error envelope: %v", rec.Body, err)
	}
	return env.Error
}
