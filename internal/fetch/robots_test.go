package fetch

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestRobotsPolicy(t *testing.T) {
	t.Parallel()

	const robots = `# comments and odd spacing are fine
User-agent: *
Disallow: /private/
Allow: /private/public-page

User-Agent: SomeOtherBot
Disallow: /

user-agent: RoNoteBot/2.0
user-agent: AnotherBot
Disallow: /drafts/
Disallow: /*.json$
Disallow: /tmp*/cache
Allow: /drafts/published/
Disallow:
`

	tests := []struct {
		name  string
		body  string
		path  string
		allow bool
	}{
		{name: "our group, unmatched path", body: robots, path: "/guide/intro", allow: true},
		{name: "our group, disallowed prefix", body: robots, path: "/drafts/next", allow: false},
		{name: "longer allow beats shorter disallow", body: robots, path: "/drafts/published/one", allow: true},
		{name: "$ anchors the end", body: robots, path: "/api/data.json", allow: false},
		{name: "$ does not match a longer path", body: robots, path: "/api/data.json?x=1", allow: true},
		{name: "* matches any run", body: robots, path: "/tmp-2026/cache/x", allow: false},
		{name: "our group replaces the * group", body: robots, path: "/private/secret", allow: true},
		{name: "robots.txt itself is always allowed", body: "User-agent: *\nDisallow: /", path: "/robots.txt", allow: true},
		{name: "* group applies without ours", body: "User-agent: *\nDisallow: /private/\nAllow: /private/public-page", path: "/private/secret", allow: false},
		{name: "allow wins a tie", body: "User-agent: *\nDisallow: /page\nAllow: /page", path: "/page", allow: true},
		{name: "empty disallow allows everything", body: "User-agent: *\nDisallow:", path: "/anything", allow: true},
		{name: "disallow everything", body: "User-agent: RoNoteBot\nDisallow: /", path: "/", allow: false},
		{name: "groups for our agent are combined", body: "User-agent: RoNoteBot\nDisallow: /a\n\nUser-agent: RoNoteBot\nDisallow: /b", path: "/b/x", allow: false},
		{name: "no robots rules", body: "", path: "/x", allow: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := parseRobots([]byte(tt.body)).allows(tt.path); got != tt.allow {
				t.Errorf("allows(%q) = %v, want %v", tt.path, got, tt.allow)
			}
		})
	}
}

func TestFetchHonoursRobots(t *testing.T) {
	t.Parallel()

	page := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<p>ok</p>"))
	})
	tests := []struct {
		name    string
		robots  http.HandlerFunc
		path    string
		wantErr error
	}{
		{name: "allowed path", robots: text("User-agent: *\nDisallow: /private/"), path: "/docs/"},
		{name: "disallowed path", robots: text("User-agent: *\nDisallow: /private/"), path: "/private/x", wantErr: ErrDisallowedByRobots},
		{name: "missing robots.txt allows", robots: status(http.StatusNotFound), path: "/docs/"},
		{name: "gone robots.txt allows", robots: status(http.StatusGone), path: "/docs/"},
		{name: "server error disallows", robots: status(http.StatusServiceUnavailable), path: "/docs/", wantErr: ErrDisallowedByRobots},
		{name: "forbidden robots.txt disallows", robots: status(http.StatusForbidden), path: "/docs/", wantErr: ErrDisallowedByRobots},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := serve(t, tt.robots, page)

			_, err := testFetcher(Options{AllowPrivateNetworks: true}).Fetch(t.Context(), Request{URL: srv.URL + tt.path})

			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil && err != nil) {
				t.Fatalf("Fetch() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestRobotsAreCachedPerHost(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name   string
		robots http.HandlerFunc
		want   int32
	}{
		{name: "a robots.txt is fetched once", robots: text("User-agent: *\nAllow: /"), want: 1},
		{name: "a 5xx answer is asked again", robots: status(http.StatusBadGateway), want: 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var robotsHits atomic.Int32
			robots := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				robotsHits.Add(1)
				tt.robots(w, r)
			})
			srv := serve(t, robots, text("<p>ok</p>"))
			f := testFetcher(Options{AllowPrivateNetworks: true})

			for range 2 {
				_, _ = f.Fetch(t.Context(), Request{URL: srv.URL + "/page"})
			}

			if got := robotsHits.Load(); got != tt.want {
				t.Errorf("robots.txt fetched %d times, want %d", got, tt.want)
			}
		})
	}
}

func TestHostLimiter(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		l := newHostLimiter(time.Second)
		start := time.Now()
		var mu sync.Mutex
		var waited []time.Duration
		var wg sync.WaitGroup
		for _, host := range []string{"a.example", "a.example", "a.example", "b.example"} {
			wg.Go(func() {
				if err := l.wait(t.Context(), host); err != nil {
					t.Error(err)
				}
				mu.Lock()
				waited = append(waited, time.Since(start))
				mu.Unlock()
			})
		}
		wg.Wait()

		slices.Sort(waited)
		want := []time.Duration{0, 0, time.Second, 2 * time.Second}
		if !slices.Equal(waited, want) {
			t.Errorf("requests started after %v, want %v", waited, want)
		}
	})
}

func TestHostLimiterStopsWithContext(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		l := newHostLimiter(time.Minute)
		if err := l.wait(t.Context(), "a.example"); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()

		if err := l.wait(ctx, "a.example"); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("wait() = %v, want context.DeadlineExceeded", err)
		}
	})
}

func TestFetchRateLimitsEachHost(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var starts []time.Time
	srv := serve(t, nil, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		starts = append(starts, time.Now())
		mu.Unlock()
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<p>ok</p>"))
	}))
	f := New(Options{AllowPrivateNetworks: true})
	f.limiter = newHostLimiter(100 * time.Millisecond)

	for range 2 {
		if _, err := f.Fetch(t.Context(), Request{URL: srv.URL}); err != nil {
			t.Fatal(err)
		}
	}

	if gap := starts[1].Sub(starts[0]); gap < 90*time.Millisecond {
		t.Errorf("second page request started %v after the first, want at least the 100ms interval", gap)
	}
}

func text(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}
}

func status(code int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(code)
	}
}
