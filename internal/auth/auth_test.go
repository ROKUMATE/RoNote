package auth

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSafeReturnTo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want string
	}{
		{"/", "/"},
		{"/doc/0192f1c4-7d3e-7c5a-9b1e-3f2a6d4c8e10", "/doc/0192f1c4-7d3e-7c5a-9b1e-3f2a6d4c8e10"},
		{"/doc/x?tab=notes#h-intro", "/doc/x?tab=notes#h-intro"},
		{"", "/"},
		{"//evil.test", "/"},
		{"//evil.test/doc/x", "/"},
		{"https://evil.test", "/"},
		{"/\\evil.test", "/"},
		{"\\\\evil.test", "/"},
		{"/doc\\..\\x", "/"},
		{"/\t/evil.test", "/"},
		{"/\n/evil.test", "/"},
		{"javascript:alert(1)", "/"},
		{"doc/x", "/"},
		{"/login", "/"},
		{"/login?return_to=/doc/x", "/"},
		{"/" + strings.Repeat("a", maxReturnTo), "/"},
	}
	for _, tt := range tests {
		if got := SafeReturnTo(tt.in); got != tt.want {
			t.Errorf("SafeReturnTo(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFlow(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)
	f := NewFlow("/doc/x", now)
	if len(f.Verifier) != 43 || f.State == "" || f.Challenge() == f.Verifier {
		t.Fatalf("NewFlow() = %+v, want a 43-character verifier, a state and a hashed challenge", f)
	}
	tampered := NewFlow("/", now)
	tampered.ReturnTo = "//evil.test"

	tests := []struct {
		name    string
		value   string
		at      time.Time
		want    Flow
		wantErr bool
	}{
		{name: "round trip", value: f.Encode(), at: now.Add(9 * time.Minute), want: f},
		{name: "older than ten minutes", value: f.Encode(), at: now.Add(11 * time.Minute), wantErr: true},
		{name: "from the future", value: f.Encode(), at: now.Add(-time.Minute), wantErr: true},
		{name: "not base64", value: "!!!", at: now, wantErr: true},
		{name: "not a flow", value: Flow{}.Encode(), at: now, wantErr: true},
		{name: "a return_to elsewhere is made safe", value: tampered.Encode(), at: now, want: Flow{State: tampered.State, Verifier: tampered.Verifier, ReturnTo: "/", Started: tampered.Started}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := DecodeFlow(tt.value, tt.at)
			if tt.wantErr {
				if err == nil {
					t.Errorf("DecodeFlow() = %+v, want ErrBadFlow", got)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Errorf("DecodeFlow() = %+v, %v; want %+v", got, err, tt.want)
			}
		})
	}
}

func TestSessionToken(t *testing.T) {
	t.Parallel()
	token, hash := NewSessionToken()
	again, ok := HashToken(token)
	if !ok || !bytes.Equal(again, hash) || len(hash) != 32 {
		t.Errorf("HashToken(new token) = %x, %v; want %x", again, ok, hash)
	}
	if other, _ := NewSessionToken(); other == token {
		t.Error("two tokens are the same")
	}
	for _, bad := range []string{"", "short", token + "A", "!" + token[1:]} {
		if _, ok := HashToken(bad); ok {
			t.Errorf("HashToken(%q) accepted a token NewSessionToken can't make", bad)
		}
	}
}

func TestCookies(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		secure   bool
		wantName string
	}{
		{name: "dev and test", wantName: "ronote_session"},
		{name: "staging and prod", secure: true, wantName: "__Host-ronote_session"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := Cookies{Secure: tt.secure}
			got := c.Session("tok")
			if got.Name != tt.wantName || got.Secure != tt.secure || !got.HttpOnly ||
				got.SameSite != http.SameSiteLaxMode || got.Path != "/" || got.Domain != "" {
				t.Errorf("session cookie = %+v, want %s, HttpOnly, Lax, Path=/, no Domain, Secure=%v", got, tt.wantName, tt.secure)
			}
			if got.MaxAge != int(SessionLifetime.Seconds()) {
				t.Errorf("session cookie MaxAge = %d, want %v", got.MaxAge, SessionLifetime)
			}
			flow := c.Flow(NewFlow("/", time.Now()))
			if flow.MaxAge != 600 || flow.Secure != tt.secure || !flow.HttpOnly || flow.Path != "/" || flow.Domain != "" {
				t.Errorf("flow cookie = %+v, want 10 minutes and the session cookie's attributes", flow)
			}
			if gone := c.ClearSession(); gone.Name != tt.wantName || gone.MaxAge >= 0 {
				t.Errorf("clearing cookie = %+v, want %s expired", gone, tt.wantName)
			}

			r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
			r.AddCookie(got)
			if token, ok := c.SessionToken(r); !ok || token != "tok" {
				t.Errorf("SessionToken() = %q, %v; want tok", token, ok)
			}
			if _, ok := (Cookies{Secure: !tt.secure}).SessionToken(r); ok {
				t.Error("a cookie of the other name was read as the session")
			}
		})
	}
}
