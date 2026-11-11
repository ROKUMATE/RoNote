//go:build integration

package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rokumate/ronote/internal/httpapi/gen"
	"github.com/rokumate/ronote/internal/pgtest"
	"github.com/rokumate/ronote/internal/store"
)

// TestSignInLinksAVerifiedEmail signs in through the fake GitHub as a user
// whose verified email an existing account has, as the dev user's will on
// first sign-in, and checks the session is that account's.
func TestSignInLinksAVerifiedEmail(t *testing.T) {
	db := pgtest.New(t)
	st := store.New(db.Pool)
	existing, err := st.CreateUser(t.Context(), "ADA@example.com", "Ada from before")
	if err != nil {
		t.Fatal(err)
	}
	rig := newSignInRig(t, false, st)

	flow, to := rig.start(t, "/")
	rec := rig.callback(t, rig.atProvider(t, to, idpAda.ID), flow)
	session := cookieNamed(rec, "ronote_session")
	if rec.Code != http.StatusSeeOther || session == nil {
		t.Fatalf("callback = %d to %q, want a session", rec.Code, rec.Header().Get("Location"))
	}

	req := newRequest(t, http.MethodGet, "/api/v1/me")
	req.AddCookie(session)
	me := record(rig.h, req)
	var got gen.Me
	if err := json.NewDecoder(me.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if me.Code != http.StatusOK || got.ID != existing.ID || got.Login != "ada" {
		t.Errorf("/me = %d %+v, want the existing account %v with login ada", me.Code, got, existing.ID)
	}
}

// TestDeletedUsersSessionsStop signs Ada in twice, deletes her account from
// one browser, and checks the other browser's session went with it.
func TestDeletedUsersSessionsStop(t *testing.T) {
	db := pgtest.New(t)
	rig := newSignInRig(t, false, store.New(db.Pool))
	signIn := func() *http.Cookie {
		flow, to := rig.start(t, "/")
		return cookieNamed(rig.callback(t, rig.atProvider(t, to, idpAda.ID), flow), "ronote_session")
	}
	here, there := signIn(), signIn()
	send := func(method, target string, c *http.Cookie, body string) int {
		req := httptest.NewRequestWithContext(t.Context(), method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(c)
		return record(rig.h, req).Code
	}
	if code := send(http.MethodGet, "/api/v1/me", there, ""); code != http.StatusOK {
		t.Fatalf("/me in the other browser = %d before deleting, want 200", code)
	}

	if code := send(http.MethodDelete, "/api/v1/me", here, `{"confirm":"ada"}`); code != http.StatusNoContent {
		t.Fatalf("DELETE /me = %d, want 204", code)
	}

	for name, c := range map[string]*http.Cookie{"this browser": here, "the other browser": there} {
		if code := send(http.MethodGet, "/api/v1/me", c, ""); code != http.StatusUnauthorized {
			t.Errorf("/me in %s after deleting = %d, want 401", name, code)
		}
	}
}
