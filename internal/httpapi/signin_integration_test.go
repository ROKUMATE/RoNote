//go:build integration

package httpapi

import (
	"encoding/json"
	"net/http"
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
