// Package auth holds the parts of sign-in that need no database: the GitHub
// OAuth client, the state and PKCE of a sign-in in progress, session tokens
// and the cookies that carry them (docs/adr/0010-sign-in-and-sessions.md).
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
)

// Session limits: a session ends after SessionIdle unused or SessionLifetime
// in all, and records its use at most once per TouchInterval.
const (
	SessionIdle     = 30 * 24 * time.Hour
	SessionLifetime = 90 * 24 * time.Hour
	TouchInterval   = 5 * time.Minute
)

// flowLifetime is how long a sign-in may take between start and callback.
const flowLifetime = 10 * time.Minute

// tokenBytes is a session token's length before encoding.
const tokenBytes = 32

// NewSessionToken returns a new session token for the cookie, and its
// SHA-256 for the database.
func NewSessionToken() (token string, hash []byte) {
	raw := make([]byte, tokenBytes)
	_, _ = rand.Read(raw) // crypto/rand.Read never fails
	sum := sha256.Sum256(raw)
	return base64.RawURLEncoding.EncodeToString(raw), sum[:]
}

// HashToken returns the SHA-256 a session token is stored under, or false
// when token is not one NewSessionToken could have made.
func HashToken(token string) ([]byte, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != tokenBytes {
		return nil, false
	}
	sum := sha256.Sum256(raw)
	return sum[:], true
}

// Flow is a sign-in in progress: what its start keeps in a cookie for its
// callback.
type Flow struct {
	State    string    `json:"s"`
	Verifier string    `json:"v"`
	ReturnTo string    `json:"r"`
	Started  time.Time `json:"t"`
}

// ErrBadFlow means a sign-in's cookie is missing, malformed or too old.
var ErrBadFlow = errors.New("no sign-in in progress")

// NewFlow starts a sign-in that returns to returnTo, made safe.
func NewFlow(returnTo string, now time.Time) Flow {
	verifier := make([]byte, 32)
	_, _ = rand.Read(verifier)
	return Flow{
		State:    rand.Text(),
		Verifier: base64.RawURLEncoding.EncodeToString(verifier),
		ReturnTo: SafeReturnTo(returnTo),
		Started:  now.UTC().Truncate(time.Second),
	}
}

// Challenge is the PKCE S256 challenge of the flow's verifier.
func (f Flow) Challenge() string {
	sum := sha256.Sum256([]byte(f.Verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// Encode returns the flow as a cookie value.
func (f Flow) Encode() string {
	data, _ := json.Marshal(f) // a struct of strings and a time always marshals
	return base64.RawURLEncoding.EncodeToString(data)
}

// DecodeFlow reads a flow from its cookie value. A flow older than ten
// minutes is ErrBadFlow, as is anything Encode didn't make.
func DecodeFlow(value string, now time.Time) (Flow, error) {
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return Flow{}, ErrBadFlow
	}
	var f Flow
	if err := json.Unmarshal(data, &f); err != nil || f.State == "" || f.Verifier == "" {
		return Flow{}, ErrBadFlow
	}
	if age := now.Sub(f.Started); age < 0 || age > flowLifetime {
		return Flow{}, ErrBadFlow
	}
	f.ReturnTo = SafeReturnTo(f.ReturnTo)
	return f, nil
}

// maxReturnTo bounds return_to, as the URL of an import is bounded.
const maxReturnTo = 2048

// SafeReturnTo returns s when it is a path on RoNote's own origin, and "/"
// otherwise: never another host, whether written //host, /\host or
// https://host, never anything with control characters, which browsers drop
// before reading a URL, and never the login page itself.
func SafeReturnTo(s string) string {
	if len(s) > maxReturnTo || !strings.HasPrefix(s, "/") || strings.ContainsRune(s, '\\') ||
		strings.HasPrefix(s, "//") || strings.ContainsFunc(s, unicode.IsControl) {
		return "/"
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil || u.Path == "/login" {
		return "/"
	}
	return s
}

// Cookies makes the session and sign-in cookies: HttpOnly, SameSite=Lax,
// Path=/ and host-only. Secure ones, for staging and prod, are also Secure
// and carry the __Host- prefix, which browsers only accept with exactly
// those attributes.
type Cookies struct {
	Secure bool
}

func (c Cookies) name(base string) string {
	if c.Secure {
		return "__Host-" + base
	}
	return base
}

func (c Cookies) cookie(base, value string, maxAge time.Duration) *http.Cookie {
	age := int(maxAge.Seconds())
	if maxAge < 0 {
		age = -1
	}
	return &http.Cookie{ //nolint:gosec // Secure in staging and prod; dev and test serve plain http
		Name: c.name(base), Value: value, Path: "/", MaxAge: age,
		HttpOnly: true, Secure: c.Secure, SameSite: http.SameSiteLaxMode,
	}
}

func (c Cookies) read(r *http.Request, base string) (string, bool) {
	cookie, err := r.Cookie(c.name(base))
	if err != nil || cookie.Value == "" {
		return "", false
	}
	return cookie.Value, true
}

// Session is the cookie that carries a session token.
func (c Cookies) Session(token string) *http.Cookie {
	return c.cookie("ronote_session", token, SessionLifetime)
}

// ClearSession is the cookie that removes the session cookie.
func (c Cookies) ClearSession() *http.Cookie {
	return c.cookie("ronote_session", "", -1)
}

// SessionToken returns the session token r carries.
func (c Cookies) SessionToken(r *http.Request) (string, bool) {
	return c.read(r, "ronote_session")
}

// Flow is the cookie that carries a sign-in in progress.
func (c Cookies) Flow(f Flow) *http.Cookie {
	return c.cookie("ronote_oauth", f.Encode(), flowLifetime)
}

// ClearFlow is the cookie that removes the sign-in cookie.
func (c Cookies) ClearFlow() *http.Cookie {
	return c.cookie("ronote_oauth", "", -1)
}

// ReadFlow returns the sign-in in progress that r's cookie carries.
func (c Cookies) ReadFlow(r *http.Request, now time.Time) (Flow, error) {
	value, ok := c.read(r, "ronote_oauth")
	if !ok {
		return Flow{}, ErrBadFlow
	}
	return DecodeFlow(value, now)
}
