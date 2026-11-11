package httpapi

import (
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/auth"
	"github.com/rokumate/ronote/internal/httpapi/gen"
	"github.com/rokumate/ronote/internal/importer"
)

// wantRateLimited checks rec is a 429 with code and a Retry-After of
// 1 to 60 seconds.
func wantRateLimited(t *testing.T, rec *httptest.ResponseRecorder, code gen.ErrorCode) {
	t.Helper()
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if got := decodeError(t, rec); got.Code != code {
		t.Errorf("code = %q, want %q", got.Code, code)
	}
	if s, err := strconv.Atoi(rec.Header().Get("Retry-After")); err != nil || s < 1 || s > 60 {
		t.Errorf("Retry-After = %q, want 1 to 60 seconds", rec.Header().Get("Retry-After"))
	}
}

func TestUserRateLimit(t *testing.T) {
	t.Parallel()
	accounts := newFakeAccounts()
	session := func(user uuid.UUID) string {
		token, hash := auth.NewSessionToken()
		accounts.sessions[hex.EncodeToString(hash)] = user
		return token
	}
	ada, grace := session(testUser), session(uuid.New())
	h := New(Options{Logger: discardLogger, DB: dbUp, Backend: &fakeBackend{}, Accounts: accounts, Limits: Limits{APIPerMinute: 3}})
	get := func(target, token string) *httptest.ResponseRecorder {
		req := newRequest(t, http.MethodGet, target)
		req.AddCookie(auth.Cookies{}.Session(token))
		rec := record(h, req)
		checkContract(t, req, rec)
		return rec
	}

	// Event streams don't count: a browser holds one per import it follows.
	for range 5 {
		get("/api/v1/imports/"+uuid.NewString()+"/events", ada)
	}
	for i := range 3 {
		if rec := get("/api/v1/documents", ada); rec.Code != http.StatusOK {
			t.Fatalf("request %d = %d, want 200", i+1, rec.Code)
		}
	}
	wantRateLimited(t, get("/api/v1/documents", ada), gen.ErrorCodeRateLimited)
	if rec := get("/api/v1/documents", grace); rec.Code != http.StatusOK {
		t.Errorf("another user's request = %d, want 200", rec.Code)
	}
}

func TestSignInRateLimit(t *testing.T) {
	t.Parallel()
	h := New(Options{Logger: discardLogger, DB: dbUp, Limits: Limits{AuthPerMinute: 2}})
	start := func(addr string) *httptest.ResponseRecorder {
		req := newRequest(t, http.MethodGet, "/api/v1/auth/github/start")
		req.RemoteAddr = addr
		return record(h, req)
	}
	for i := range 2 {
		if rec := start("192.0.2.1:4000"); rec.Code != http.StatusFound {
			t.Fatalf("sign-in %d = %d, want 302", i+1, rec.Code)
		}
	}
	// Another port is the same IP.
	wantRateLimited(t, start("192.0.2.1:4001"), gen.ErrorCodeRateLimited)
	if rec := start("192.0.2.2:4000"); rec.Code != http.StatusFound {
		t.Errorf("another IP's sign-in = %d, want 302", rec.Code)
	}
}

func TestQuotaExceeded(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		err        error
		retryAfter string
	}{
		{name: "imports per day", err: &importer.QuotaError{Quota: "imports per day", RetryAfter: 90*time.Minute + time.Millisecond}, retryAfter: "5401"},
		{name: "documents", err: &importer.QuotaError{Quota: "documents"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := &fakeBackend{request: func(string) (importer.Requested, error) { return importer.Requested{}, tt.err }}
			h := signedIn(Options{Logger: discardLogger, DB: dbUp, Backend: b})

			rec := apiRequest(t, h, http.MethodPost, "/api/v1/imports", `{"url":"https://docs.example.com/guide"}`)

			if rec.Code != http.StatusTooManyRequests || decodeError(t, rec).Code != gen.ErrorCodeQuotaExceeded {
				t.Fatalf("status = %d %s, want 429 quota_exceeded", rec.Code, rec.Body)
			}
			if got := rec.Header().Get("Retry-After"); got != tt.retryAfter {
				t.Errorf("Retry-After = %q, want %q", got, tt.retryAfter)
			}
		})
	}
}

func TestBodyLimits(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		method  string
		target  string
		size    int
		chunked bool // the body doesn't say its length
		tooBig  bool
	}{
		{name: "an import, past 64 KiB", method: http.MethodPost, target: "/api/v1/imports", size: maxBody + 1, tooBig: true},
		{name: "an import, past 64 KiB, chunked", method: http.MethodPost, target: "/api/v1/imports", size: maxBody + 1, chunked: true, tooBig: true},
		{name: "an import, within", method: http.MethodPost, target: "/api/v1/imports", size: 100},
		{name: "a document of 2 MiB", method: http.MethodPatch, target: "/api/v1/documents/" + uuid.NewString(), size: 2 << 20},
		{name: "a document, past 8 MiB", method: http.MethodPatch, target: "/api/v1/documents/" + uuid.NewString(), size: maxDocumentBody + 1, tooBig: true},
		{name: "re-anchoring, past 1 MiB", method: http.MethodPost, target: "/api/v1/documents/" + uuid.NewString() + "/annotations/reanchor", size: maxReanchorBody + 1, tooBig: true},
		{name: "an annotation, past 64 KiB", method: http.MethodPost, target: "/api/v1/documents/" + uuid.NewString() + "/annotations", size: maxBody + 1, tooBig: true},
		{name: "deleting the account, past 64 KiB, chunked", method: http.MethodDelete, target: "/api/v1/me", size: maxBody + 1, chunked: true, tooBig: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// A JSON string as long as the size, so only its length is wrong.
			body := `{"url":"` + strings.Repeat("a", max(tt.size-10, 0)) + `"}`
			var r io.Reader = strings.NewReader(body)
			if tt.chunked {
				r = io.MultiReader(r) // hides the length
			}
			req := httptest.NewRequestWithContext(t.Context(), tt.method, tt.target, r)
			req.Header.Set("Content-Type", "application/json")
			if tt.chunked {
				req.ContentLength = -1
			}
			b := &fakeBackend{request: func(string) (importer.Requested, error) { return importer.Requested{}, importer.ErrInvalidURL }}

			rec := record(signedIn(Options{Logger: discardLogger, DB: dbUp, Backend: b}), req)

			if tooBig := rec.Code == http.StatusRequestEntityTooLarge; tooBig != tt.tooBig {
				t.Fatalf("status = %d, want 413 = %v", rec.Code, tt.tooBig)
			}
			if tt.tooBig {
				checkContract(t, req, rec)
				if got := decodeError(t, rec); got.Code != gen.ErrorCodeBodyTooLarge {
					t.Errorf("code = %q, want body_too_large", got.Code)
				}
			}
		})
	}
}
