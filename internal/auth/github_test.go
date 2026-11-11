package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rokumate/ronote/internal/fakeidp"
	"github.com/rokumate/ronote/internal/store"
)

var (
	ada        = fakeidp.User{ID: 1, Login: "ada", Name: "Ada Lovelace", Email: "ada@example.com", Verified: true}
	unverified = fakeidp.User{ID: 2, Login: "nemo", Email: "nemo@example.com"}
)

// newGitHub returns a GitHub client for an OAuth app at a fake provider.
func newGitHub(t *testing.T) GitHub {
	t.Helper()
	idp := httptest.NewServer(fakeidp.New(fakeidp.Options{ClientID: "id", ClientSecret: "secret", Users: []fakeidp.User{ada, unverified}}))
	t.Cleanup(idp.Close)
	return GitHub{
		ClientID: "id", ClientSecret: "secret",
		AuthorizeURL: idp.URL + "/login/oauth/authorize", TokenURL: idp.URL + "/login/oauth/access_token",
		APIURL: idp.URL, RedirectURL: "http://app.test/api/v1/auth/github/callback",
	}
}

// signInAt walks the provider's page as user and returns the code it sends
// back.
func signInAt(t *testing.T, g GitHub, f Flow, user int64) string {
	t.Helper()
	auth, err := url.Parse(g.AuthURL(f))
	if err != nil {
		t.Fatal(err)
	}
	form := auth.Query()
	form.Set("user", strconv.FormatInt(user, 10))
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, g.AuthorizeURL, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	back, err := url.Parse(res.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return back.Query().Get("code")
}

func TestAuthURL(t *testing.T) {
	t.Parallel()
	g := GitHub{ClientID: "id", AuthorizeURL: "https://github.com/login/oauth/authorize", RedirectURL: "https://ronote.test/cb"}
	f := NewFlow("/", time.Now())
	u, err := url.Parse(g.AuthURL(f))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	want := map[string]string{
		"client_id": "id", "redirect_uri": "https://ronote.test/cb", "scope": "read:user user:email",
		"state": f.State, "code_challenge": f.Challenge(), "code_challenge_method": "S256",
	}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
	if q.Has("code_verifier") {
		t.Error("the verifier went to the browser")
	}
}

func TestSignInWithGitHub(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		user    int64
		tamper  func(f *Flow)
		want    store.Identity
		wantErr error
	}{
		{
			name: "a user with a verified email",
			user: 1,
			want: store.Identity{Provider: "github", ProviderUserID: "1", Login: "ada", Email: "ada@example.com", DisplayName: "Ada Lovelace"},
		},
		{name: "no verified email", user: 2, wantErr: ErrNoVerifiedEmail},
		{name: "another verifier than the one challenged", user: 1, tamper: func(f *Flow) { f.Verifier += "x" }, wantErr: ErrDenied},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := newGitHub(t)
			f := NewFlow("/", time.Now())
			code := signInAt(t, g, f, tt.user)
			if tt.tamper != nil {
				tt.tamper(&f)
			}

			token, err := g.Exchange(t.Context(), code, f)
			var got store.Identity
			if err == nil {
				got, err = g.Identity(t.Context(), token)
			}

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("identity = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestBadCode(t *testing.T) {
	t.Parallel()
	g := newGitHub(t)
	if _, err := g.Exchange(t.Context(), "made-up", NewFlow("/", time.Now())); !errors.Is(err, ErrDenied) {
		t.Errorf("Exchange(bad code) = %v, want ErrDenied", err)
	}
}

func TestGitHubUnavailable(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{name: "a server error", handler: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }},
		{name: "not JSON", handler: func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>")) }},
		{name: "too large", handler: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"access_token":"` + strings.Repeat("x", maxResponse) + `"}`))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(tt.handler)
			t.Cleanup(srv.Close)
			g := GitHub{TokenURL: srv.URL, APIURL: srv.URL}
			if _, err := g.Exchange(t.Context(), "code", NewFlow("/", time.Now())); !errors.Is(err, ErrUnavailable) {
				t.Errorf("Exchange() = %v, want ErrUnavailable", err)
			}
			if _, err := g.Identity(t.Context(), "token"); !errors.Is(err, ErrUnavailable) {
				t.Errorf("Identity() = %v, want ErrUnavailable", err)
			}
		})
	}
}
