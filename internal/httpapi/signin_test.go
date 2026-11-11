package httpapi

import (
	"bytes"
	"context"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/auth"
	"github.com/rokumate/ronote/internal/fakeidp"
	"github.com/rokumate/ronote/internal/store"
)

var (
	idpAda        = fakeidp.User{ID: 1, Login: "ada", Name: "Ada", Email: "ada@example.com", Verified: true}
	idpUnverified = fakeidp.User{ID: 2, Login: "nemo", Email: "nemo@example.com"}
)

// fakeAccounts keeps users and sessions in memory.
type fakeAccounts struct {
	mu       sync.Mutex
	users    map[string]store.User // by provider user ID
	sessions map[string]uuid.UUID  // by hex token hash
}

func newFakeAccounts() *fakeAccounts {
	return &fakeAccounts{users: map[string]store.User{}, sessions: map[string]uuid.UUID{}}
}

func (f *fakeAccounts) SignIn(_ context.Context, id store.Identity) (store.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[id.ProviderUserID]
	if !ok {
		u = store.User{ID: uuid.New(), Email: id.Email, DisplayName: id.DisplayName}
		f.users[id.ProviderUserID] = u
	}
	return u, nil
}

func (f *fakeAccounts) CreateSession(_ context.Context, hash []byte, user uuid.UUID, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions[hex.EncodeToString(hash)] = user
	return nil
}

func (f *fakeAccounts) DeleteSession(_ context.Context, hash []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.sessions, hex.EncodeToString(hash))
	return nil
}

// sessionOf returns the user of the session cookie, if it is one.
func (f *fakeAccounts) sessionOf(c *http.Cookie) (uuid.UUID, bool) {
	hash, ok := auth.HashToken(c.Value)
	if !ok {
		return uuid.Nil, false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.sessions[hex.EncodeToString(hash)]
	return u, ok
}

// signInRig is the API with sign-in at a fake GitHub.
type signInRig struct {
	h        http.Handler
	idp      *httptest.Server
	accounts *fakeAccounts
	cookies  auth.Cookies

	mu        sync.Mutex
	verifiers []string // the code_verifier of every token exchange
}

func newSignInRig(t *testing.T, secure bool) *signInRig {
	t.Helper()
	rig := &signInRig{accounts: newFakeAccounts(), cookies: auth.Cookies{Secure: secure}}
	provider := fakeidp.New(fakeidp.Options{ClientID: "id", ClientSecret: "secret", Users: []fakeidp.User{idpAda, idpUnverified}})
	rig.idp = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login/oauth/access_token" {
			_ = r.ParseForm()
			rig.mu.Lock()
			rig.verifiers = append(rig.verifiers, r.PostForm.Get("code_verifier"))
			rig.mu.Unlock()
		}
		provider.ServeHTTP(w, r)
	}))
	t.Cleanup(rig.idp.Close)
	rig.h = New(Options{Logger: discardLogger, DB: dbUp, SignIn: SignIn{
		GitHub: auth.GitHub{
			ClientID: "id", ClientSecret: "secret",
			AuthorizeURL: rig.idp.URL + "/login/oauth/authorize", TokenURL: rig.idp.URL + "/login/oauth/access_token",
			APIURL: rig.idp.URL, RedirectURL: "http://app.test/api/v1/auth/github/callback",
		},
		Cookies:  rig.cookies,
		Accounts: rig.accounts,
	}})
	return rig
}

// cookieNamed returns the cookie rec set under a name ending in name.
func cookieNamed(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if strings.HasSuffix(c.Name, name) {
			return c
		}
	}
	return nil
}

// start begins a sign-in and returns its cookie and where it sends the
// browser.
func (rig *signInRig) start(t *testing.T, returnTo string) (*http.Cookie, *url.URL) {
	t.Helper()
	rec := record(rig.h, newRequest(t, http.MethodGet, "/api/v1/auth/github/start?return_to="+url.QueryEscape(returnTo)))
	if rec.Code != http.StatusFound {
		t.Fatalf("start = %d, want 302", rec.Code)
	}
	to, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	flow := cookieNamed(rec, "ronote_oauth")
	if flow == nil {
		t.Fatal("start set no sign-in cookie")
	}
	return flow, to
}

// atProvider submits the provider's page, choosing user or, with 0, Deny,
// and returns the callback's query.
func (rig *signInRig) atProvider(t *testing.T, to *url.URL, user int64) url.Values {
	t.Helper()
	form := to.Query()
	if user == 0 {
		form.Set("deny", "1")
	} else {
		form.Set("user", strconv.FormatInt(user, 10))
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, rig.idp.URL+"/login/oauth/authorize", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	back, err := url.Parse(res.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return back.Query()
}

func (rig *signInRig) callback(t *testing.T, query url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := newRequest(t, http.MethodGet, "/api/v1/auth/github/callback?"+query.Encode())
	for _, c := range cookies {
		req.AddCookie(c)
	}
	return record(rig.h, req)
}

func TestSignIn(t *testing.T) {
	t.Parallel()
	for _, secure := range []bool{false, true} {
		t.Run("secure="+strconv.FormatBool(secure), func(t *testing.T) {
			t.Parallel()
			rig := newSignInRig(t, secure)

			flow, to := rig.start(t, "/doc/x?tab=notes")
			rec := rig.callback(t, rig.atProvider(t, to, idpAda.ID), flow)

			if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/doc/x?tab=notes" {
				t.Fatalf("callback = %d to %q, want 303 to /doc/x?tab=notes", rec.Code, rec.Header().Get("Location"))
			}
			session := cookieNamed(rec, "ronote_session")
			if session == nil {
				t.Fatal("no session cookie")
			}
			if user, ok := rig.accounts.sessionOf(session); !ok || user != rig.accounts.users["1"].ID {
				t.Errorf("the cookie's session is %v, %v; want Ada's", user, ok)
			}
			header := strings.Join(rec.Result().Header.Values("Set-Cookie"), "\n")
			for _, want := range []string{"HttpOnly", "SameSite=Lax", "Path=/"} {
				if !strings.Contains(header, want) {
					t.Errorf("Set-Cookie lacks %s:\n%s", want, header)
				}
			}
			if strings.Contains(header, "Domain=") {
				t.Errorf("Set-Cookie names a Domain:\n%s", header)
			}
			if prefixed := strings.HasPrefix(session.Name, "__Host-"); prefixed != secure || session.Secure != secure {
				t.Errorf("session cookie %s, Secure=%v; want __Host- and Secure exactly when secure (%v)", session.Name, session.Secure, secure)
			}
			if gone := cookieNamed(rec, "ronote_oauth"); gone == nil || gone.MaxAge >= 0 {
				t.Errorf("sign-in cookie after the callback = %+v, want it removed", gone)
			}
			decoded, err := auth.DecodeFlow(flow.Value, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			rig.mu.Lock()
			defer rig.mu.Unlock()
			if len(rig.verifiers) != 1 || rig.verifiers[0] != decoded.Verifier {
				t.Errorf("the token exchange sent verifiers %q, want the flow's %q", rig.verifiers, decoded.Verifier)
			}
		})
	}
}

func TestSignInFailures(t *testing.T) {
	t.Parallel()
	expired := func(t *testing.T, rig *signInRig, flow *http.Cookie, back url.Values) (url.Values, []*http.Cookie) {
		t.Helper()
		f, err := auth.DecodeFlow(flow.Value, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		old := auth.NewFlow(f.ReturnTo, time.Now().Add(-11*time.Minute))
		old.State, old.Verifier = f.State, f.Verifier
		return back, []*http.Cookie{rig.cookies.Flow(old)}
	}
	tests := []struct {
		name string
		user int64 // 0 denies at the provider
		// change alters the callback's query and cookies.
		change   func(t *testing.T, rig *signInRig, flow *http.Cookie, back url.Values) (url.Values, []*http.Cookie)
		wantCode string
		// lost is set when no usable cookie says where to return to.
		lost bool
	}{
		{
			name: "no sign-in cookie", user: 1, wantCode: "state_mismatch", lost: true,
			change: func(_ *testing.T, _ *signInRig, _ *http.Cookie, back url.Values) (url.Values, []*http.Cookie) {
				return back, nil
			},
		},
		{name: "a sign-in cookie older than ten minutes", user: 1, change: expired, wantCode: "state_mismatch", lost: true},
		{
			name: "another state", user: 1, wantCode: "state_mismatch",
			change: func(_ *testing.T, _ *signInRig, flow *http.Cookie, back url.Values) (url.Values, []*http.Cookie) {
				back.Set("state", "forged")
				return back, []*http.Cookie{flow}
			},
		},
		{
			name: "no state", user: 1, wantCode: "state_mismatch",
			change: func(_ *testing.T, _ *signInRig, flow *http.Cookie, back url.Values) (url.Values, []*http.Cookie) {
				back.Del("state")
				return back, []*http.Cookie{flow}
			},
		},
		{name: "denied at GitHub", user: 0, wantCode: "denied"},
		{
			name: "a bad code", user: 1, wantCode: "denied",
			change: func(_ *testing.T, _ *signInRig, flow *http.Cookie, back url.Values) (url.Values, []*http.Cookie) {
				back.Set("code", "made-up")
				return back, []*http.Cookie{flow}
			},
		},
		{name: "no verified email", user: idpUnverified.ID, wantCode: "no_verified_email"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rig := newSignInRig(t, false)
			flow, to := rig.start(t, "/doc/x")
			back := rig.atProvider(t, to, tt.user)
			cookies := []*http.Cookie{flow}
			if tt.change != nil {
				back, cookies = tt.change(t, rig, flow, back)
			}

			rec := rig.callback(t, back, cookies...)

			want := "/login?error=" + tt.wantCode + "&return_to=%2Fdoc%2Fx"
			if tt.lost {
				want = "/login?error=" + tt.wantCode
			}
			if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != want {
				t.Errorf("callback = %d to %q, want 303 to %q", rec.Code, rec.Header().Get("Location"), want)
			}
			if cookieNamed(rec, "ronote_session") != nil || len(rig.accounts.sessions) != 0 {
				t.Error("a failed sign-in started a session")
			}
		})
	}
}

func TestSignInWithGitHubDown(t *testing.T) {
	t.Parallel()
	rig := newSignInRig(t, false)
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	t.Cleanup(down.Close)
	flow, to := rig.start(t, "/")
	back := rig.atProvider(t, to, idpAda.ID)
	rig.h = New(Options{Logger: discardLogger, DB: dbUp, SignIn: SignIn{
		GitHub:  auth.GitHub{ClientID: "id", ClientSecret: "secret", TokenURL: down.URL, APIURL: down.URL},
		Cookies: rig.cookies, Accounts: rig.accounts,
	}})

	rec := rig.callback(t, back, flow)

	if rec.Header().Get("Location") != "/login?error=provider_unavailable" {
		t.Errorf("callback to %q, want /login?error=provider_unavailable", rec.Header().Get("Location"))
	}
}

func TestSignInEndsTheEarlierSession(t *testing.T) {
	t.Parallel()
	rig := newSignInRig(t, false)
	first := func() *http.Cookie {
		flow, to := rig.start(t, "/")
		return cookieNamed(rig.callback(t, rig.atProvider(t, to, idpAda.ID), flow), "ronote_session")
	}()

	flow, to := rig.start(t, "/")
	rec := rig.callback(t, rig.atProvider(t, to, idpAda.ID), flow, first)

	second := cookieNamed(rec, "ronote_session")
	if second == nil || second.Value == first.Value {
		t.Fatalf("second sign-in's cookie = %+v, want a new token", second)
	}
	if _, ok := rig.accounts.sessionOf(first); ok {
		t.Error("the session from before signing in again still works")
	}
	if _, ok := rig.accounts.sessionOf(second); !ok {
		t.Error("the new session doesn't work")
	}
}

func TestStartReturnTo(t *testing.T) {
	t.Parallel()
	tests := []struct{ in, want string }{
		{"/doc/x", "/doc/x"},
		{"", "/"},
		{"//evil.test", "/"},
		{"https://evil.test", "/"},
		{"/\\evil.test", "/"},
	}
	rig := newSignInRig(t, false)
	for _, tt := range tests {
		flow, to := rig.start(t, tt.in)
		f, err := auth.DecodeFlow(flow.Value, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if f.ReturnTo != tt.want {
			t.Errorf("start?return_to=%q kept %q, want %q", tt.in, f.ReturnTo, tt.want)
		}
		if !strings.HasPrefix(to.String(), rig.idp.URL+"/login/oauth/authorize?") || to.Query().Get("state") != f.State ||
			!bytes.Equal([]byte(to.Query().Get("code_challenge")), []byte(f.Challenge())) {
			t.Errorf("start sent the browser to %v, want the provider with the flow's state and challenge", to)
		}
	}
}
