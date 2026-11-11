package httpapi

import (
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/auth"
	"github.com/rokumate/ronote/internal/httpapi/gen"
	"github.com/rokumate/ronote/internal/store"
)

// accountRig is the API with the tester signed in twice, in two browsers,
// and Grace once.
type accountRig struct {
	h                  http.Handler
	accounts           *fakeAccounts
	here, there, grace string // session tokens
}

func newAccountRig() accountRig {
	accounts := newFakeAccounts()
	accounts.users["1"] = store.User{ID: testUser, Email: "tester@example.com", DisplayName: "Tester"}
	accounts.logins[testUser] = "Tester"
	grace := uuid.New()
	accounts.users["2"] = store.User{ID: grace, Email: "grace@example.com", DisplayName: "Grace"}
	session := func(user uuid.UUID) string {
		token, hash := auth.NewSessionToken()
		accounts.sessions[hex.EncodeToString(hash)] = user
		return token
	}
	return accountRig{
		h:        New(Options{Logger: discardLogger, DB: dbUp, Backend: &fakeBackend{}, Accounts: accounts}),
		accounts: accounts,
		here:     session(testUser), there: session(testUser), grace: session(grace),
	}
}

func (rig accountRig) send(t *testing.T, method, target, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.AddCookie(auth.Cookies{}.Session(token))
	rec := record(rig.h, req)
	checkContract(t, req, rec)
	return rec
}

// works reports whether token's session still signs requests in.
func (rig accountRig) works(t *testing.T, token string) bool {
	t.Helper()
	return rig.send(t, http.MethodGet, "/api/v1/documents", token, "").Code == http.StatusOK
}

// wantSignedOut checks rec signed this browser out: 204, cookie removed.
func wantSignedOut(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body)
	}
	if c := cookieNamed(rec, "ronote_session"); c == nil || c.MaxAge >= 0 {
		t.Errorf("session cookie = %+v, want it removed", c)
	}
}

func TestLogout(t *testing.T) {
	t.Parallel()
	rig := newAccountRig()

	wantSignedOut(t, rig.send(t, http.MethodPost, "/api/v1/auth/logout", rig.here, ""))

	if rig.works(t, rig.here) || !rig.works(t, rig.there) || !rig.works(t, rig.grace) {
		t.Errorf("after signing out here: here %v, there %v, Grace %v; want only here ended",
			rig.works(t, rig.here), rig.works(t, rig.there), rig.works(t, rig.grace))
	}
}

func TestLogoutAll(t *testing.T) {
	t.Parallel()
	rig := newAccountRig()

	wantSignedOut(t, rig.send(t, http.MethodPost, "/api/v1/auth/logout-all", rig.here, ""))

	if rig.works(t, rig.here) || rig.works(t, rig.there) || !rig.works(t, rig.grace) {
		t.Errorf("after signing out everywhere: here %v, there %v, Grace %v; want both of the tester's ended",
			rig.works(t, rig.here), rig.works(t, rig.there), rig.works(t, rig.grace))
	}
}

func TestSignOutNeedsASession(t *testing.T) {
	t.Parallel()
	rig := newAccountRig()
	for _, target := range []string{"/api/v1/auth/logout", "/api/v1/auth/logout-all"} {
		if rec := rig.send(t, http.MethodPost, target, "", ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without a session = %d, want 401", target, rec.Code)
		}
	}
}

func TestDeleteMe(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		body    string
		deleted bool
	}{
		{name: "the login", body: `{"confirm":"Tester"}`, deleted: true},
		{name: "the login in another case", body: `{"confirm":"tester"}`, deleted: true},
		{name: "another login", body: `{"confirm":"grace"}`},
		{name: "an empty confirm", body: `{"confirm":""}`},
		{name: "no JSON", body: `confirm=Tester`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rig := newAccountRig()

			rec := rig.send(t, http.MethodDelete, "/api/v1/me", rig.here, tt.body)

			if !tt.deleted {
				if rec.Code != http.StatusBadRequest || decodeError(t, rec).Code != gen.ErrorCodeInvalidRequest {
					t.Fatalf("status = %d %s, want 400 invalid_request", rec.Code, rec.Body)
				}
				if !rig.works(t, rig.here) {
					t.Error("a refused deletion ended the session")
				}
				return
			}
			wantSignedOut(t, rec)
			if rig.works(t, rig.here) || rig.works(t, rig.there) || !rig.works(t, rig.grace) {
				t.Error("after deleting the account its sessions still work, or Grace's stopped")
			}
			if _, err := rig.accounts.GetAccount(t.Context(), testUser, auth.Provider); err == nil {
				t.Error("the account is still there")
			}
		})
	}
}
