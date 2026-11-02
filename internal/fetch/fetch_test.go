package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// serve starts a test server that answers /robots.txt with robots, or 404
// when robots is nil, and every other path with h.
func serve(t *testing.T, robots, h http.Handler) *httptest.Server {
	t.Helper()
	if robots == nil {
		robots = http.NotFoundHandler()
	}
	mux := http.NewServeMux()
	mux.Handle("/robots.txt", robots)
	mux.Handle("/", h)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// testFetcher is New without the per-host rate limit, which tests that are
// not about it would only wait on.
func testFetcher(opts Options) *Fetcher {
	f := New(opts)
	f.limiter = newHostLimiter(0)
	return f
}

// mapResolver answers lookups from a fixed table, so tests never use DNS.
type mapResolver map[string][]netip.Addr

func (m mapResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	if ips, ok := m[host]; ok {
		return ips, nil
	}
	return nil, fmt.Errorf("no such host %s", host)
}

func TestAddressGuard(t *testing.T) {
	t.Parallel()

	tests := []struct {
		addr          string
		blockedStrict bool // with production rules
		blockedDev    bool // with AllowPrivateNetworks
	}{
		{addr: "93.184.215.14:443"},
		{addr: "[2606:2800:21f:cb07:6820:80da:af6b:8b2c]:443"},
		{addr: "127.0.0.1:443", blockedStrict: true},
		{addr: "127.8.9.10:80", blockedStrict: true},
		{addr: "[::1]:443", blockedStrict: true},
		{addr: "10.1.2.3:443", blockedStrict: true},
		{addr: "172.16.0.1:443", blockedStrict: true},
		{addr: "172.31.255.255:443", blockedStrict: true},
		{addr: "192.168.1.1:443", blockedStrict: true},
		{addr: "169.254.10.20:443", blockedStrict: true},
		{addr: "[fe80::1]:443", blockedStrict: true},
		{addr: "100.64.0.1:443", blockedStrict: true},
		{addr: "100.127.255.254:443", blockedStrict: true},
		{addr: "[fd00:ec2::254]:443", blockedStrict: true},
		{addr: "[fc00::1]:443", blockedStrict: true},
		{addr: "[64:ff9b::a00:1]:443", blockedStrict: true},
		{addr: "169.254.169.254:80", blockedStrict: true, blockedDev: true},
		{addr: "0.0.0.0:443", blockedStrict: true, blockedDev: true},
		{addr: "[::]:443", blockedStrict: true, blockedDev: true},
		{addr: "224.0.0.1:443", blockedStrict: true, blockedDev: true},
		{addr: "239.255.255.250:443", blockedStrict: true, blockedDev: true},
		{addr: "[ff02::1]:443", blockedStrict: true, blockedDev: true},
		{addr: "255.255.255.255:443", blockedStrict: true, blockedDev: true},
		{addr: "[::ffff:127.0.0.1]:443", blockedStrict: true, blockedDev: true},
		{addr: "[::ffff:93.184.215.14]:443", blockedStrict: true, blockedDev: true},
	}

	strict, dev := addressGuard(false), addressGuard(true)
	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			t.Parallel()
			ap := netip.MustParseAddrPort(tt.addr)
			if got := errors.Is(strict(ap), ErrBlockedAddress); got != tt.blockedStrict {
				t.Errorf("production guard blocked = %v, want %v", got, tt.blockedStrict)
			}
			if got := errors.Is(dev(ap), ErrBlockedAddress); got != tt.blockedDev {
				t.Errorf("dev guard blocked = %v, want %v", got, tt.blockedDev)
			}
		})
	}
}

// TestFetchRefusesBlockedAddresses goes through Fetch, so the guard runs in
// the dialer's Control hook. Every case is refused before a connection opens.
func TestFetchRefusesBlockedAddresses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		url     string
		wantErr error
	}{
		{name: "loopback", url: "http://127.0.0.1/", wantErr: ErrBlockedAddress},
		{name: "IPv6 loopback", url: "http://[::1]/", wantErr: ErrBlockedAddress},
		{name: "private", url: "https://10.0.0.5/", wantErr: ErrBlockedAddress},
		{name: "link-local", url: "http://169.254.1.1/", wantErr: ErrBlockedAddress},
		{name: "metadata", url: "http://169.254.169.254/latest/meta-data/", wantErr: ErrBlockedAddress},
		{name: "CGNAT", url: "http://100.64.1.1/", wantErr: ErrBlockedAddress},
		{name: "multicast", url: "http://224.0.0.251/", wantErr: ErrBlockedAddress},
		{name: "unspecified", url: "http://0.0.0.0/", wantErr: ErrBlockedAddress},
		{name: "IPv6 unique local", url: "http://[fd12:3456::1]/", wantErr: ErrBlockedAddress},
		{name: "IPv4-mapped IPv6", url: "http://[::ffff:10.0.0.1]/", wantErr: ErrBlockedAddress},
		{name: "hostname resolving to a private address", url: "https://docs.internal.example/", wantErr: ErrBlockedAddress},
		{name: "port other than 80 or 443", url: "http://docs.example.org:8080/", wantErr: ErrBlockedAddress},
		{name: "scheme other than http(s)", url: "ftp://docs.example.org/", wantErr: ErrInvalidURL},
		{name: "file URL", url: "file:///etc/passwd", wantErr: ErrInvalidURL},
		{name: "user info in the URL", url: "http://user@docs.example.org/", wantErr: ErrInvalidURL},
	}

	f := testFetcher(Options{Resolver: mapResolver{"docs.internal.example": {netip.MustParseAddr("10.1.2.3")}}})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := f.Fetch(t.Context(), Request{URL: tt.url})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Fetch(%s) error = %v, want %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestRedirectToPrivateAddressIsRefused(t *testing.T) {
	t.Parallel()
	private := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("the private server was reached")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(private.Close)
	public := serve(t, nil, http.RedirectHandler(private.URL+"/secret", http.StatusFound))

	// Production rules, except that the test's "public" server counts as
	// public and may use its random port.
	f := testFetcher(Options{})
	f.allowAnyPort = true
	publicAddr := netip.MustParseAddrPort(strings.TrimPrefix(public.URL, "http://"))
	strict := f.guard
	f.guard = func(ap netip.AddrPort) error {
		if ap == publicAddr {
			return nil
		}
		return strict(ap)
	}

	_, err := f.Fetch(t.Context(), Request{URL: public.URL})
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("Fetch() error = %v, want ErrBlockedAddress", err)
	}
}

func TestFetch(t *testing.T) {
	t.Parallel()

	const page = "<!doctype html><title>T</title><p>Hello</p>"
	html := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Last-Modified", "Mon, 02 Nov 2026 10:00:00 GMT")
		_, _ = w.Write([]byte(page))
	}

	tests := []struct {
		name       string
		handler    http.HandlerFunc
		request    func(base string) Request
		wantErr    error
		wantStatus int
		check      func(t *testing.T, res *Result)
	}{
		{
			name:    "HTML page",
			handler: html,
			check: func(t *testing.T, res *Result) {
				if string(res.Body) != page || res.ETag != `"v1"` || res.LastModified == "" {
					t.Errorf("result = %+v", res)
				}
				if sum := sha256.Sum256([]byte(page)); res.BodySHA256 != hex.EncodeToString(sum[:]) {
					t.Errorf("BodySHA256 = %q, want the SHA-256 of the body", res.BodySHA256)
				}
			},
		},
		{
			name: "sends the RoNoteBot user agent",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.UserAgent() != "RoNoteBot/0.1 (+https://github.com/ROKUMATE/RoNote)" {
					http.Error(w, r.UserAgent(), http.StatusTeapot)
					return
				}
				html(w, r)
			},
		},
		{
			name:    "five redirects are followed",
			handler: redirects(5, html),
			check: func(t *testing.T, res *Result) {
				if !strings.HasSuffix(res.FinalURL, "/hop-5") {
					t.Errorf("FinalURL = %q, want the fifth hop", res.FinalURL)
				}
			},
		},
		{
			name:    "six redirects are refused",
			handler: redirects(6, html),
			wantErr: ErrTooManyRedirects,
		},
		{
			name: "declared size over the limit",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				w.Header().Set("Content-Length", fmt.Sprint(6<<20))
				_, _ = w.Write(make([]byte, 6<<20))
			},
			wantErr: ErrTooLarge,
		},
		{
			name: "streamed size over the limit",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				chunk := []byte(strings.Repeat("a", 1<<20))
				for range 6 {
					_, _ = w.Write(chunk)
					w.(http.Flusher).Flush()
				}
			},
			wantErr: ErrTooLarge,
		},
		{
			name: "JSON is not fetched",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{}`))
			},
			wantErr: ErrUnsupportedType,
		},
		{
			name: "missing content type is sniffed",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header()["Content-Type"] = nil
				_, _ = w.Write([]byte(page))
			},
			check: func(t *testing.T, res *Result) {
				if !strings.HasPrefix(res.ContentType, "text/html") {
					t.Errorf("ContentType = %q", res.ContentType)
				}
			},
		},
		{
			name: "windows-1252 is decoded to UTF-8",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=windows-1252")
				_, _ = w.Write([]byte("<p>caf\xe9 \x93quoted\x94</p>"))
			},
			check: func(t *testing.T, res *Result) {
				if string(res.Body) != "<p>café “quoted”</p>" {
					t.Errorf("Body = %q", res.Body)
				}
			},
		},
		{
			name: "meta charset is honoured",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				_, _ = w.Write([]byte("<meta charset=\"iso-8859-1\"><p>na\xefve</p>"))
			},
			check: func(t *testing.T, res *Result) {
				if !strings.Contains(string(res.Body), "naïve") {
					t.Errorf("Body = %q", res.Body)
				}
			},
		},
		{
			name: "unchanged page with a conditional request",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("If-None-Match") == `"v1"` && r.Header.Get("If-Modified-Since") != "" {
					w.WriteHeader(http.StatusNotModified)
					return
				}
				html(w, r)
			},
			request: func(base string) Request {
				return Request{URL: base, ETag: `"v1"`, LastModified: "Mon, 02 Nov 2026 10:00:00 GMT"}
			},
			check: func(t *testing.T, res *Result) {
				if !res.NotModified || len(res.Body) != 0 {
					t.Errorf("result = %+v, want NotModified with no body", res)
				}
			},
		},
		{
			name:       "error status",
			handler:    http.NotFound,
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := serve(t, nil, tt.handler)
			req := Request{URL: srv.URL}
			if tt.request != nil {
				req = tt.request(srv.URL)
			}

			res, err := testFetcher(Options{AllowPrivateNetworks: true}).Fetch(t.Context(), req)

			if tt.wantStatus != 0 {
				var status *StatusError
				if !errors.As(err, &status) || status.Code != tt.wantStatus {
					t.Fatalf("Fetch() error = %v, want StatusError %d", err, tt.wantStatus)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil && err != nil) {
				t.Fatalf("Fetch() error = %v, want %v", err, tt.wantErr)
			}
			if tt.check != nil {
				tt.check(t, res)
			}
		})
	}
}

// redirects answers the first n requests with a redirect to the next of a
// fixed set of paths, then serves final.
func redirects(n int32, final http.HandlerFunc) http.HandlerFunc {
	var hits atomic.Int32
	hops := []string{"/hop-1", "/hop-2", "/hop-3", "/hop-4", "/hop-5", "/hop-6"}
	return func(w http.ResponseWriter, r *http.Request) {
		if hit := hits.Add(1); hit <= n {
			http.Redirect(w, r, hops[hit-1], http.StatusFound)
			return
		}
		final(w, r)
	}
}

func TestFetchTimesOut(t *testing.T) {
	t.Parallel()
	srv := serve(t, nil, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	f := testFetcher(Options{AllowPrivateNetworks: true})
	f.totalTimeout = 100 * time.Millisecond

	_, err := f.Fetch(t.Context(), Request{URL: srv.URL})

	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("Fetch() error = %v, want ErrTimeout", err)
	}
}
