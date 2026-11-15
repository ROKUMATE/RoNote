// Package fakeidp is a stand-in for GitHub's OAuth app endpoints, for dev and
// tests only (docs/adr/0010-sign-in-and-sessions.md). It serves the four
// endpoints RoNote's sign-in calls, with GitHub's paths and shapes, for a
// list of test users, which a test can add to when Registration is on;
// config keeps it out of staging and prod.
package fakeidp

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// User is a test user the sign-in page offers.
type User struct {
	ID       int64  `json:"id"`
	Login    string `json:"login"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Verified bool   `json:"verified"`
}

// LoadUsers reads a JSON array of users from path.
func LoadUsers(path string) ([]User, error) {
	data, err := os.ReadFile(path) //nolint:gosec // a path the developer passes in
	if err != nil {
		return nil, err
	}
	var users []User
	if err := json.Unmarshal(data, &users); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(users) == 0 {
		return nil, fmt.Errorf("%s: no users", path)
	}
	return users, nil
}

// Options are the OAuth app the provider knows and the users it offers.
type Options struct {
	ClientID     string
	ClientSecret string
	Users        []User
	// Registration serves POST /test/users, which adds a user the sign-in
	// page then offers: the end-to-end tests give each test a library of
	// its own with it. Off unless asked for, so dev doesn't offer it.
	Registration bool
}

// codeLifetime is how long an authorization code may wait for its exchange.
const codeLifetime = 10 * time.Minute

// grant is an authorization code waiting for its exchange.
type grant struct {
	user        User
	redirectURI string
	challenge   string
	expires     time.Time
}

type provider struct {
	opts Options

	mu     sync.Mutex
	users  []User // the listed users, then the registered ones
	nextID int64  // the id of the next registered user
	codes  map[string]grant
	tokens map[string]User
}

// New returns the provider's handler.
func New(o Options) http.Handler {
	// Registered ids count up from the start time in microseconds: RoNote
	// keys users by the provider's id, and its database outlives the
	// provider, so a later run must not give a new user an earlier one's id.
	// The listed users' are small numbers, clear of these.
	p := &provider{opts: o, users: slices.Clone(o.Users), nextID: time.Now().UnixMicro(), codes: map[string]grant{}, tokens: map[string]User{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok\n")) })
	mux.HandleFunc("GET /login/oauth/authorize", p.authorizePage)
	mux.HandleFunc("POST /login/oauth/authorize", p.authorize)
	mux.HandleFunc("POST /login/oauth/access_token", p.accessToken)
	mux.HandleFunc("GET /user", p.user)
	mux.HandleFunc("GET /user/emails", p.emails)
	if o.Registration {
		mux.HandleFunc("POST /test/users", p.register)
	}
	return mux
}

// request is what the app sent to the authorize endpoint.
type request struct {
	RedirectURI string
	State       string
	Challenge   string
}

func (p *provider) readRequest(form url.Values) (request, error) {
	switch {
	case form.Get("client_id") != p.opts.ClientID:
		return request{}, fmt.Errorf("unknown client_id %q", form.Get("client_id"))
	case form.Get("code_challenge_method") != "S256" || form.Get("code_challenge") == "":
		return request{}, fmt.Errorf("an S256 code_challenge is required")
	case form.Get("state") == "":
		return request{}, fmt.Errorf("state is required")
	}
	redirect, err := url.Parse(form.Get("redirect_uri"))
	if err != nil || !redirect.IsAbs() {
		return request{}, fmt.Errorf("redirect_uri %q is not an absolute URL", form.Get("redirect_uri"))
	}
	return request{RedirectURI: redirect.String(), State: form.Get("state"), Challenge: form.Get("code_challenge")}, nil
}

var page = template.Must(template.New("authorize").Parse(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Fake GitHub: sign in to RoNote</title></head>
<body>
<main>
<h1>Fake GitHub</h1>
<p>A stand-in provider for development and tests. Choose who signs in to RoNote.</p>
<form method="post" action="/login/oauth/authorize">
<input type="hidden" name="client_id" value="{{.ClientID}}">
<input type="hidden" name="redirect_uri" value="{{.RedirectURI}}">
<input type="hidden" name="state" value="{{.State}}">
<input type="hidden" name="code_challenge" value="{{.Challenge}}">
<input type="hidden" name="code_challenge_method" value="S256">
<ul>
{{range .Users}}<li><button type="submit" name="user" value="{{.ID}}">Sign in as {{.Login}}</button> {{.Email}}{{if not .Verified}} (unverified){{end}}</li>
{{end}}</ul>
<button type="submit" name="deny" value="1">Deny</button>
</form>
</main>
</body>
</html>
`))

func (p *provider) authorizePage(w http.ResponseWriter, r *http.Request) {
	req, err := p.readRequest(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p.mu.Lock()
	users := slices.Clone(p.users)
	p.mu.Unlock()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = page.Execute(w, struct {
		request
		ClientID string
		Users    []User
	}{req, p.opts.ClientID, users})
}

func (p *provider) authorize(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	req, err := p.readRequest(r.PostForm)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	back := url.Values{"state": {req.State}}
	if r.PostForm.Get("deny") != "" {
		back.Set("error", "access_denied")
		back.Set("error_description", "The user has denied your application access.")
		http.Redirect(w, r, req.RedirectURI+"?"+back.Encode(), http.StatusFound)
		return
	}
	user, ok := p.find(r.PostForm.Get("user"))
	if !ok {
		http.Error(w, "unknown user", http.StatusBadRequest)
		return
	}
	code := rand.Text()
	p.mu.Lock()
	p.codes[code] = grant{user: user, redirectURI: req.RedirectURI, challenge: req.Challenge, expires: time.Now().Add(codeLifetime)}
	p.mu.Unlock()
	back.Set("code", code)
	http.Redirect(w, r, req.RedirectURI+"?"+back.Encode(), http.StatusFound)
}

func (p *provider) find(id string) (User, bool) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return User{}, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, u := range p.users {
		if u.ID == n {
			return u, true
		}
	}
	return User{}, false
}

// register adds a verified user, and answers with it: 409 when its login or
// email, compared without case, is already a user's.
func (p *provider) register(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Login string `json:"login"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil || in.Login == "" || in.Email == "" {
		http.Error(w, "want JSON with a login, a name and an email", http.StatusBadRequest)
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, u := range p.users {
		if strings.EqualFold(u.Login, in.Login) || strings.EqualFold(u.Email, in.Email) {
			http.Error(w, "that login or email is already a user's", http.StatusConflict)
			return
		}
	}
	u := User{ID: p.nextID, Login: in.Login, Name: in.Name, Email: in.Email, Verified: true}
	p.nextID++
	p.users = append(p.users, u)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(u)
}

// accessToken exchanges a code, as GitHub does: a refusal is a 200 with an
// error field.
func (p *provider) accessToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f := r.PostForm
	if f.Get("client_id") != p.opts.ClientID ||
		subtle.ConstantTimeCompare([]byte(f.Get("client_secret")), []byte(p.opts.ClientSecret)) != 1 {
		writeJSON(w, map[string]string{"error": "incorrect_client_credentials"})
		return
	}
	code := f.Get("code")
	p.mu.Lock()
	g, ok := p.codes[code]
	delete(p.codes, code) // a code is good for one exchange, whatever its outcome
	p.mu.Unlock()
	if !ok || time.Now().After(g.expires) || f.Get("redirect_uri") != g.redirectURI {
		writeJSON(w, map[string]string{"error": "bad_verification_code"})
		return
	}
	sum := sha256.Sum256([]byte(f.Get("code_verifier")))
	if base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
		writeJSON(w, map[string]string{"error": "bad_verification_code", "error_description": "The code_verifier does not match the code_challenge."})
		return
	}
	token := rand.Text()
	p.mu.Lock()
	p.tokens[token] = g.user
	p.mu.Unlock()
	writeJSON(w, map[string]string{"access_token": token, "token_type": "bearer", "scope": "read:user,user:email"})
}

func (p *provider) bearer(w http.ResponseWriter, r *http.Request) (User, bool) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	p.mu.Lock()
	u, known := p.tokens[token]
	p.mu.Unlock()
	if !ok || !known {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
		return User{}, false
	}
	return u, true
}

func (p *provider) user(w http.ResponseWriter, r *http.Request) {
	if u, ok := p.bearer(w, r); ok {
		writeJSON(w, map[string]any{"id": u.ID, "login": u.Login, "name": u.Name})
	}
}

func (p *provider) emails(w http.ResponseWriter, r *http.Request) {
	if u, ok := p.bearer(w, r); ok {
		writeJSON(w, []map[string]any{{"email": u.Email, "primary": true, "verified": u.Verified, "visibility": "private"}})
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
