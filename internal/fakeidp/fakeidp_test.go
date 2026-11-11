package fakeidp

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const redirectURI = "http://app.test/callback"

var ada = User{ID: 1, Login: "ada", Name: "Ada", Email: "ada@example.com", Verified: true}

func newProvider(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(New(Options{ClientID: "id", ClientSecret: "secret", Users: []User{ada}}))
	t.Cleanup(srv.Close)
	return srv
}

func challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// reply is a response, read and closed.
type reply struct {
	status int
	header http.Header
	body   []byte
}

// send makes a request through client and returns its response.
func send(t *testing.T, client *http.Client, method, target string, form url.Values, token string) reply {
	t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(t.Context(), method, target, body)
	if err != nil {
		t.Fatal(err)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(res.Body)
	if cerr := res.Body.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		t.Fatal(err)
	}
	return reply{status: res.StatusCode, header: res.Header, body: data}
}

// noRedirects is a client that reports redirects instead of following them.
var noRedirects = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// choose submits the sign-in page as a person would and returns where the
// provider sends the browser back to.
func choose(t *testing.T, srv *httptest.Server, button url.Values) *url.URL {
	t.Helper()
	form := url.Values{
		"client_id": {"id"}, "redirect_uri": {redirectURI}, "state": {"s1"},
		"code_challenge": {challenge("verifier")}, "code_challenge_method": {"S256"},
	}
	for k, v := range button {
		form[k] = v
	}
	res := send(t, noRedirects, http.MethodPost, srv.URL+"/login/oauth/authorize", form, "")
	if res.status != http.StatusFound {
		t.Fatalf("authorize = %d, want 302", res.status)
	}
	back, err := url.Parse(res.header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if back.Query().Get("state") != "s1" {
		t.Errorf("state = %q, want it returned unchanged", back.Query().Get("state"))
	}
	return back
}

func exchange(t *testing.T, srv *httptest.Server, code, verifier string) map[string]string {
	t.Helper()
	res := send(t, http.DefaultClient, http.MethodPost, srv.URL+"/login/oauth/access_token", url.Values{
		"client_id": {"id"}, "client_secret": {"secret"}, "code": {code},
		"redirect_uri": {redirectURI}, "code_verifier": {verifier},
	}, "")
	var body map[string]string
	if err := json.Unmarshal(res.body, &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func get(t *testing.T, srv *httptest.Server, path, token string, v any) int {
	t.Helper()
	res := send(t, http.DefaultClient, http.MethodGet, srv.URL+path, nil, token)
	if res.status == http.StatusOK {
		if err := json.Unmarshal(res.body, v); err != nil {
			t.Fatal(err)
		}
	}
	return res.status
}

func TestSignInFlow(t *testing.T) {
	t.Parallel()
	srv := newProvider(t)

	back := choose(t, srv, url.Values{"user": {"1"}})
	tok := exchange(t, srv, back.Query().Get("code"), "verifier")

	var user struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
	}
	if status := get(t, srv, "/user", tok["access_token"], &user); status != http.StatusOK || user.ID != 1 || user.Login != "ada" {
		t.Errorf("/user = %d %+v, want Ada", status, user)
	}
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if status := get(t, srv, "/user/emails", tok["access_token"], &emails); status != http.StatusOK ||
		len(emails) != 1 || emails[0].Email != "ada@example.com" || !emails[0].Primary || !emails[0].Verified {
		t.Errorf("/user/emails = %d %+v, want Ada's primary, verified email", status, emails)
	}
}

func TestExchangeRefusals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		verifier string
		reuse    bool
	}{
		{name: "a verifier that doesn't match the challenge", verifier: "other"},
		{name: "a code used twice", verifier: "verifier", reuse: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := newProvider(t)
			code := choose(t, srv, url.Values{"user": {"1"}}).Query().Get("code")
			if tt.reuse {
				exchange(t, srv, code, tt.verifier)
			}
			if got := exchange(t, srv, code, tt.verifier); got["error"] != "bad_verification_code" || got["access_token"] != "" {
				t.Errorf("exchange = %v, want bad_verification_code", got)
			}
		})
	}
}

func TestDeny(t *testing.T) {
	t.Parallel()
	back := choose(t, newProvider(t), url.Values{"deny": {"1"}})
	if back.Query().Get("error") != "access_denied" || back.Query().Get("code") != "" {
		t.Errorf("redirected to %v, want error=access_denied and no code", back)
	}
}

func TestAuthorizePage(t *testing.T) {
	t.Parallel()
	srv := newProvider(t)
	tests := []struct {
		name   string
		query  string
		status int
	}{
		{name: "lists the users", query: "client_id=id&redirect_uri=http%3A%2F%2Fapp.test%2Fcallback&state=s&code_challenge=c&code_challenge_method=S256", status: http.StatusOK},
		{name: "refuses plain PKCE", query: "client_id=id&redirect_uri=http%3A%2F%2Fapp.test%2Fcallback&state=s&code_challenge=c&code_challenge_method=plain", status: http.StatusBadRequest},
		{name: "refuses an unknown client", query: "client_id=x&redirect_uri=http%3A%2F%2Fapp.test%2Fcallback&state=s&code_challenge=c&code_challenge_method=S256", status: http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res := send(t, http.DefaultClient, http.MethodGet, srv.URL+"/login/oauth/authorize?"+tt.query, nil, "")
			page := res.body
			if res.status != tt.status {
				t.Fatalf("status = %d, want %d", res.status, tt.status)
			}
			if tt.status == http.StatusOK && (!strings.Contains(string(page), "Sign in as ada") || !strings.Contains(string(page), "Deny")) {
				t.Errorf("page lacks Ada's button or Deny:\n%s", page)
			}
		})
	}
}

func TestUnknownToken(t *testing.T) {
	t.Parallel()
	if status := get(t, newProvider(t), "/user", "nope", nil); status != http.StatusUnauthorized {
		t.Errorf("/user with an unknown token = %d, want 401", status)
	}
}
