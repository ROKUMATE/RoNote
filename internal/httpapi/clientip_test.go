package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestClientIP(t *testing.T) {
	t.Parallel()
	caddy := Proxies{netip.MustParsePrefix("10.89.10.0/24"), netip.MustParsePrefix("fd00:89::/64")}
	tests := []struct {
		name    string
		proxies Proxies
		remote  string
		xff     []string
		want    string
	}{
		{name: "no proxy trusted: the peer", remote: "203.0.113.7:5000", want: "203.0.113.7"},
		{name: "a spoofed header from an untrusted peer is ignored", proxies: caddy, remote: "203.0.113.7:5000", xff: []string{"198.51.100.1"}, want: "203.0.113.7"},
		{name: "a header is ignored when no proxy is trusted", remote: "10.89.10.2:5000", xff: []string{"198.51.100.1"}, want: "10.89.10.2"},
		{name: "through the trusted proxy", proxies: caddy, remote: "10.89.10.2:5000", xff: []string{"198.51.100.1"}, want: "198.51.100.1"},
		// The client claimed 192.0.2.66 itself; Caddy appended who really
		// connected. Only the hops added by trusted proxies count.
		{name: "a client's own header before the proxy's hop", proxies: caddy, remote: "10.89.10.2:5000", xff: []string{"192.0.2.66, 198.51.100.1"}, want: "198.51.100.1"},
		{name: "a trusted chain: the first hop that isn't trusted", proxies: caddy, remote: "10.89.10.2:5000", xff: []string{"192.0.2.66, 198.51.100.1, 10.89.10.9"}, want: "198.51.100.1"},
		{name: "headers sent twice read as one list", proxies: caddy, remote: "10.89.10.2:5000", xff: []string{"192.0.2.66", "198.51.100.1"}, want: "198.51.100.1"},
		{name: "every hop trusted: the first", proxies: caddy, remote: "10.89.10.2:5000", xff: []string{"10.89.10.3, 10.89.10.4"}, want: "10.89.10.3"},
		{name: "no header from the proxy: the proxy", proxies: caddy, remote: "10.89.10.2:5000", want: "10.89.10.2"},
		{name: "a malformed hop: the last good one", proxies: caddy, remote: "10.89.10.2:5000", xff: []string{"198.51.100.1, not-an-ip"}, want: "10.89.10.2"},
		{name: "IPv6 through the proxy", proxies: caddy, remote: "[fd00:89::2]:5000", xff: []string{"2001:db8::1"}, want: "2001:db8::1"},
		{name: "an IPv4-mapped peer is its IPv4", proxies: caddy, remote: "[::ffff:10.89.10.2]:5000", xff: []string{"198.51.100.1"}, want: "198.51.100.1"},
		{name: "an unparsable peer as it is", proxies: caddy, remote: "pipe", want: "pipe"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)
			r.RemoteAddr = tt.remote
			for _, v := range tt.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			if got := tt.proxies.clientIP(r); got != tt.want {
				t.Errorf("clientIP = %q, want %q", got, tt.want)
			}
		})
	}
}

// Behind Caddy every connection is Caddy's: the sign-in limit counts the
// clients it forwards for, and a header from anyone else changes nothing.
func TestSignInRateLimitBehindATrustedProxy(t *testing.T) {
	t.Parallel()
	h := New(Options{
		Logger: discardLogger, DB: dbUp, Limits: Limits{AuthPerMinute: 1},
		TrustedProxies: Proxies{netip.MustParsePrefix("10.89.10.0/24")},
	})
	start := func(remote, xff string) int {
		req := newRequest(t, http.MethodGet, "/api/v1/auth/github/start")
		req.RemoteAddr = remote
		req.Header.Set("X-Forwarded-For", xff)
		return record(h, req).Code
	}
	if code := start("10.89.10.2:4000", "198.51.100.1"); code != http.StatusFound {
		t.Fatalf("first client = %d, want 302", code)
	}
	if code := start("10.89.10.2:4000", "198.51.100.2"); code != http.StatusFound {
		t.Errorf("a second client through the same proxy = %d, want 302: its own limit", code)
	}
	if code := start("10.89.10.2:4000", "198.51.100.1"); code != http.StatusTooManyRequests {
		t.Errorf("the first client again = %d, want 429", code)
	}
	if code := start("203.0.113.7:4000", "198.51.100.3"); code != http.StatusFound {
		t.Fatalf("an untrusted peer = %d, want 302", code)
	}
	if code := start("203.0.113.7:4000", "198.51.100.4"); code != http.StatusTooManyRequests {
		t.Errorf("the untrusted peer claiming another client = %d, want 429: its header is ignored", code)
	}
}
