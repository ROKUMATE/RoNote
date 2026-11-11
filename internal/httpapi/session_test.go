package httpapi

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/auth"
	"github.com/rokumate/ronote/internal/httpapi/gen"
)

func TestRequireSession(t *testing.T) {
	t.Parallel()
	live, liveHash := auth.NewSessionToken()
	ended, _ := auth.NewSessionToken()
	tests := []struct {
		name   string
		target string
		cookie string // "" sends none
		want   int
	}{
		{name: "no cookie", target: "/api/v1/documents", want: http.StatusUnauthorized},
		{name: "no cookie, event stream", target: "/api/v1/imports/" + uuid.NewString() + "/events", want: http.StatusUnauthorized},
		{name: "not a token", target: "/api/v1/documents", cookie: "garbage", want: http.StatusUnauthorized},
		{name: "a token with no session", target: "/api/v1/documents", cookie: ended, want: http.StatusUnauthorized},
		{name: "a live session", target: "/api/v1/documents", cookie: live, want: http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			accounts := newFakeAccounts()
			accounts.sessions[hex.EncodeToString(liveHash)] = testUser
			b := &fakeBackend{}
			h := New(Options{Logger: discardLogger, DB: dbUp, Backend: b, Accounts: accounts})
			req := newRequest(t, http.MethodGet, tt.target)
			if tt.cookie != "" {
				req.AddCookie(auth.Cookies{}.Session(tt.cookie))
			}

			rec := record(h, req)

			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d", rec.Code, tt.want)
			}
			if tt.want == http.StatusUnauthorized {
				if got := decodeError(t, rec); got.Code != gen.ErrorCodeUnauthenticated {
					t.Errorf("code = %q, want unauthenticated", got.Code)
				}
				if len(b.owners) != 0 {
					t.Errorf("the backend was asked, for %v", b.owners)
				}
				return
			}
			if len(b.owners) != 1 || b.owners[0] != testUser {
				t.Errorf("asked for owners %v, want the session's user", b.owners)
			}
		})
	}
}

func TestSessionTouch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		stale       bool
		wantTouches int
	}{
		{name: "seen within five minutes", wantTouches: 0},
		{name: "seen longer ago", stale: true, wantTouches: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			accounts := newFakeAccounts()
			token, hash := auth.NewSessionToken()
			accounts.sessions[hex.EncodeToString(hash)] = testUser
			accounts.stale[hex.EncodeToString(hash)] = tt.stale
			h := New(Options{Logger: discardLogger, DB: dbUp, Backend: &fakeBackend{}, Accounts: accounts})

			for range 3 {
				req := newRequest(t, http.MethodGet, "/api/v1/documents")
				req.AddCookie(auth.Cookies{}.Session(token))
				if rec := record(h, req); rec.Code != http.StatusOK {
					t.Fatalf("status = %d, want 200", rec.Code)
				}
			}

			if accounts.touched != tt.wantTouches {
				t.Errorf("touched %d times in 3 requests, want %d", accounts.touched, tt.wantTouches)
			}
		})
	}
}

func TestGetMe(t *testing.T) {
	t.Parallel()
	req := newRequest(t, http.MethodGet, "/api/v1/me")
	rec := record(signedIn(Options{Logger: discardLogger, DB: dbUp, Backend: &fakeBackend{}}), req)

	checkContract(t, req, rec)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var me gen.Me
	if err := json.NewDecoder(rec.Body).Decode(&me); err != nil {
		t.Fatal(err)
	}
	if me.ID != testUser || me.Login != "tester" || me.DisplayName != "Tester" {
		t.Errorf("me = %+v, want the tester", me)
	}
}
