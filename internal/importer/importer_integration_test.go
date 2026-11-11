//go:build integration

package importer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/rokumate/ronote/internal/doccontent"
	"github.com/rokumate/ronote/internal/fetch"
	"github.com/rokumate/ronote/internal/jobs"
	"github.com/rokumate/ronote/internal/jobs/riverjobs"
	"github.com/rokumate/ronote/internal/pgtest"
	"github.com/rokumate/ronote/internal/store"
)

var testDB *pgtest.DB

func TestMain(m *testing.M) {
	pgtest.Main(m, &testDB)
}

var discard = slog.New(slog.DiscardHandler)

// loopback resolves the test site's names to 127.0.0.1, so no DNS is used.
type loopback struct{}

func (loopback) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	if host == "docs.test" || host == "other.test" {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

func htmlPage(title, code string) string {
	return "<!doctype html><html><head><title>" + title + "</title></head><body><main><h1>" + title +
		"</h1><p>Run this:</p><pre><code class=\"language-go\">" + code + "</code></pre></main></body></html>"
}

// site is the documentation site imports fetch from. Pages answer
// conditional requests with 304; a path can be told to fail first.
type site struct {
	srv *httptest.Server

	mu          sync.Mutex
	robotsDown  int // robots.txt answers 503 this many more times
	pages       map[string]string
	failures    map[string][]int // statuses to answer with before the page
	retryAfter  string
	hits        map[string][]time.Time
	notModified int
}

func newSite(t *testing.T) *site {
	t.Helper()
	s := &site{pages: map[string]string{}, failures: map[string][]int{}, hits: map[string][]time.Time{}}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *site) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.URL.Path == "/robots.txt" {
		if s.robotsDown > 0 {
			s.robotsDown--
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = fmt.Fprint(w, "User-agent: *\nDisallow: /private\n")
		return
	}
	key := r.Host + r.URL.Path
	s.hits[key] = append(s.hits[key], time.Now())
	if queued := s.failures[key]; len(queued) > 0 {
		s.failures[key] = queued[1:]
		if s.retryAfter != "" {
			w.Header().Set("Retry-After", s.retryAfter)
		}
		w.WriteHeader(queued[0])
		return
	}
	body, ok := s.pages[key]
	if !ok {
		http.NotFound(w, r)
		return
	}
	sum := sha256.Sum256([]byte(body))
	etag := `"` + hex.EncodeToString(sum[:8]) + `"`
	if r.Header.Get("If-None-Match") == etag {
		s.notModified++
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprint(w, body)
}

// url returns the address of path on host, a name loopback resolves.
func (s *site) url(host, path string) string {
	_, port, _ := net.SplitHostPort(s.srv.Listener.Addr().String())
	return "http://" + host + ":" + port + path
}

func (s *site) set(host, path, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, port, _ := net.SplitHostPort(s.srv.Listener.Addr().String())
	s.pages[host+":"+port+path] = body
}

func (s *site) failFirst(host, path string, statuses ...int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, port, _ := net.SplitHostPort(s.srv.Listener.Addr().String())
	s.failures[host+":"+port+path] = statuses
}

// hitTimes lists when docs.test served path, robots.txt aside.
func (s *site) hitTimes(path string) []time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, port, _ := net.SplitHostPort(s.srv.Listener.Addr().String())
	return append([]time.Time(nil), s.hits["docs.test:"+port+path]...)
}

// harness runs a real worker on River against the test database and site.
type harness struct {
	t          *testing.T
	store      *store.Store
	service    *Service
	queue      *riverjobs.Queue
	stop       func()
	site       *site
	ada, grace store.User
}

func newHarness(t *testing.T, ttl time.Duration) *harness {
	t.Helper()
	testDB.Reset(t)
	h := &harness{t: t, store: store.New(testDB.Pool), site: newSite(t)}
	ctx := t.Context()
	var err error
	if h.ada, err = h.store.CreateUser(ctx, "ada@example.com", "Ada"); err != nil {
		t.Fatal(err)
	}
	if h.grace, err = h.store.CreateUser(ctx, "grace@example.com", "Grace"); err != nil {
		t.Fatal(err)
	}
	fetcher := fetch.New(fetch.Options{AllowPrivateNetworks: true, Resolver: loopback{}})
	worker := NewWorker(h.store, fetcher, ttl, discard)
	// Failed jobs retry after 100ms instead of River's backoff.
	h.queue, err = riverjobs.New(testDB.Pool, riverjobs.Options{
		Logger:            discard,
		RetryDelay:        func(int) time.Duration { return 100 * time.Millisecond },
		FetchCooldown:     20 * time.Millisecond,
		FetchPollInterval: 50 * time.Millisecond,
		StopTimeout:       10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	h.queue.Handle(Kind, worker.Handle, JobOptions)
	h.start()
	t.Cleanup(func() { h.stop() })
	h.service = NewService(h.store, h.queue)
	return h
}

// start runs the worker until stop.
func (h *harness) start() {
	ctx, cancel := context.WithCancel(context.WithoutCancel(h.t.Context()))
	done := make(chan error, 1)
	go func() { done <- h.queue.Run(ctx) }()
	h.stop = func() {
		cancel()
		if err := <-done; err != nil {
			h.t.Errorf("stop River: %v", err)
		}
		h.stop = func() {}
	}
}

// enqueue enqueues an import's job again, without its key, as a job
// delivered twice would run.
func (h *harness) enqueue(args Args) {
	h.t.Helper()
	payload, err := json.Marshal(args)
	if err != nil {
		h.t.Fatal(err)
	}
	err = h.store.InTx(h.t.Context(), func(_ *store.Store, raw pgx.Tx) error {
		return h.queue.EnqueueTx(h.t.Context(), raw, jobs.Job{Kind: Kind, Payload: payload})
	})
	if err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) request(owner store.User, url string) Requested {
	h.t.Helper()
	req, err := h.service.Request(h.t.Context(), owner.ID, url)
	if err != nil {
		h.t.Fatalf("Request(%s): %v", url, err)
	}
	return req
}

// wait returns the import once it has finished.
func (h *harness) wait(owner store.User, id uuid.UUID) store.Import {
	h.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		imp, err := h.store.GetImport(h.t.Context(), owner.ID, id)
		if err != nil {
			h.t.Fatal(err)
		}
		if imp.Status.Terminal() {
			return imp
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("import %s still %s after 20s", id, imp.Status)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func (h *harness) count(table string) int {
	h.t.Helper()
	var n int
	if err := testDB.Pool.QueryRow(h.t.Context(), "SELECT count(*) FROM "+pgx.Identifier{table}.Sanitize()).Scan(&n); err != nil {
		h.t.Fatal(err)
	}
	return n
}

// jobs lists the state and attempt of every job for an import.
func (h *harness) jobs(id uuid.UUID) []string {
	h.t.Helper()
	rows, err := testDB.Pool.Query(h.t.Context(),
		"SELECT state || '/' || attempt FROM river_job WHERE args->'payload'->>'import_id' = $1 ORDER BY id", id.String())
	if err != nil {
		h.t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		h.t.Fatal(err)
	}
	return out
}

// waitJobs waits until every job of an import has left the running states.
func (h *harness) waitJobs(id uuid.UUID) []string {
	h.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		jobs := h.jobs(id)
		settled := len(jobs) > 0
		for _, j := range jobs {
			state, _, _ := strings.Cut(j, "/")
			settled = settled && (state == "completed" || state == "cancelled" || state == "discarded")
		}
		if settled || time.Now().After(deadline) {
			return jobs
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func TestImport(t *testing.T) {
	h := newHarness(t, time.Hour)
	h.site.set("docs.test", "/guide", htmlPage("Guide", "fmt.Println(\"hi\")\n"))
	url := h.site.url("docs.test", "/guide")

	req := h.request(h.ada, url+"?utm_source=newsletter#install")
	if req.Existing || req.Import.Status != store.ImportQueued || req.Import.CanonicalURL != url {
		t.Fatalf("Request() = %+v", req)
	}
	imp := h.wait(h.ada, req.Import.ID)
	if imp.Status != store.ImportDone {
		t.Fatalf("import = %+v, want done", imp)
	}
	doc, err := h.store.GetDocument(t.Context(), h.ada.ID, imp.DocumentID)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Title != "Guide" || doc.SourceURL != url || doccontent.Validate(doc.Content) != nil ||
		len(doc.Content.Blocks) != 3 || doc.Content.Blocks[2].Text != "fmt.Println(\"hi\")\n" {
		t.Errorf("document = %+v with %d blocks", doc.DocumentSummary, len(doc.Content.Blocks))
	}
	extraction, err := h.store.DocumentExtraction(t.Context(), h.ada.ID, imp.DocumentID)
	if err != nil {
		t.Fatal(err)
	}
	if extraction.Source.URL != url {
		t.Errorf("IR source URL = %q, want the canonical %q, not the user's exact request", extraction.Source.URL, url)
	}
	for table, want := range map[string]int{"sources": 1, "source_snapshots": 1, "extractions": 1, "documents": 1, "imports": 1} {
		if got := h.count(table); got != want {
			t.Errorf("%s rows = %d, want %d", table, got, want)
		}
	}
	if hits := h.site.hitTimes("/guide"); len(hits) != 1 {
		t.Errorf("site hits = %d, want 1", len(hits))
	}
	if jobs := h.waitJobs(imp.ID); fmt.Sprint(jobs) != "[completed/1]" {
		t.Errorf("jobs = %v, want one completed on the first attempt", jobs)
	}

	t.Run("same user again gets the same document without a fetch", func(t *testing.T) {
		for _, again := range []string{url, url + "#other", url + "?gclid=x"} {
			req := h.request(h.ada, again)
			if !req.Existing || req.Import.ID != imp.ID || req.Import.DocumentID != imp.DocumentID {
				t.Errorf("Request(%s) = %+v, want the existing import and document", again, req)
			}
		}
		if got := h.count("imports"); got != 1 {
			t.Errorf("imports = %d, want no new one", got)
		}
		if hits := h.site.hitTimes("/guide"); len(hits) != 1 {
			t.Errorf("site hits = %d, want still 1", len(hits))
		}
	})

	t.Run("another user shares the cache within the TTL", func(t *testing.T) {
		req := h.request(h.grace, url)
		got := h.wait(h.grace, req.Import.ID)
		if got.Status != store.ImportDone || got.DocumentID == imp.DocumentID {
			t.Fatalf("Grace's import = %+v, want done with a document of her own", got)
		}
		for table, want := range map[string]int{"sources": 1, "source_snapshots": 1, "extractions": 1, "documents": 2} {
			if n := h.count(table); n != want {
				t.Errorf("%s rows = %d, want %d", table, n, want)
			}
		}
		if hits := h.site.hitTimes("/guide"); len(hits) != 1 {
			t.Errorf("site hits = %d, want 1: a fresh source is not fetched again", len(hits))
		}
	})
}

func TestRevalidation(t *testing.T) {
	h := newHarness(t, 0) // every import asks the site again
	h.site.set("docs.test", "/guide", htmlPage("Guide", "v1()\n"))
	url := h.site.url("docs.test", "/guide")
	first := h.wait(h.ada, h.request(h.ada, url).Import.ID)

	t.Run("304 reuses the stored snapshot", func(t *testing.T) {
		imp := h.wait(h.grace, h.request(h.grace, url).Import.ID)
		if imp.Status != store.ImportDone {
			t.Fatalf("import = %+v", imp)
		}
		if hits, n := len(h.site.hitTimes("/guide")), h.site.notModified; hits != 2 || n != 1 {
			t.Errorf("site hits = %d with %d 304s, want 2 with 1", hits, n)
		}
		if snaps, exts := h.count("source_snapshots"), h.count("extractions"); snaps != 1 || exts != 1 {
			t.Errorf("snapshots = %d, extractions = %d; want 1 and 1", snaps, exts)
		}
	})

	t.Run("changed content makes a new snapshot", func(t *testing.T) {
		h.site.set("docs.test", "/guide", htmlPage("Guide, revised", "v2()\n"))
		lin, err := h.store.CreateUser(t.Context(), "lin@example.com", "Lin")
		if err != nil {
			t.Fatal(err)
		}
		imp := h.wait(lin, h.request(lin, url).Import.ID)
		doc, err := h.store.GetDocument(t.Context(), lin.ID, imp.DocumentID)
		if err != nil {
			t.Fatal(err)
		}
		if doc.Title != "Guide, revised" {
			t.Errorf("new document title = %q, want the revised page", doc.Title)
		}
		if snaps, exts, srcs := h.count("source_snapshots"), h.count("extractions"), h.count("sources"); snaps != 2 || exts != 2 || srcs != 1 {
			t.Errorf("snapshots = %d, extractions = %d, sources = %d; want 2, 2, 1", snaps, exts, srcs)
		}
		old, err := h.store.GetDocument(t.Context(), h.ada.ID, first.DocumentID)
		if err != nil || old.Title != "Guide" {
			t.Errorf("Ada's document = %q, %v; want it unchanged", old.Title, err)
		}
	})
}

func TestRetries(t *testing.T) {
	h := newHarness(t, time.Hour)

	t.Run("503 then success", func(t *testing.T) {
		h.site.set("docs.test", "/flaky", htmlPage("Flaky", "ok()\n"))
		h.site.failFirst("docs.test", "/flaky", http.StatusServiceUnavailable)
		imp := h.wait(h.ada, h.request(h.ada, h.site.url("docs.test", "/flaky")).Import.ID)
		if imp.Status != store.ImportDone {
			t.Fatalf("import = %+v, want done", imp)
		}
		if jobs := h.waitJobs(imp.ID); fmt.Sprint(jobs) != "[completed/2]" {
			t.Errorf("jobs = %v, want completed on the second attempt", jobs)
		}
	})

	t.Run("429 waits for Retry-After", func(t *testing.T) {
		h.site.set("docs.test", "/busy", htmlPage("Busy", "ok()\n"))
		h.site.failFirst("docs.test", "/busy", http.StatusTooManyRequests)
		h.site.mu.Lock()
		h.site.retryAfter = "2"
		h.site.mu.Unlock()
		imp := h.wait(h.ada, h.request(h.ada, h.site.url("docs.test", "/busy")).Import.ID)
		if imp.Status != store.ImportDone {
			t.Fatalf("import = %+v, want done", imp)
		}
		hits := h.site.hitTimes("/busy")
		if len(hits) != 2 || hits[1].Sub(hits[0]) < 2*time.Second {
			t.Errorf("hits = %v, want a retry at least 2s after the 429", hits)
		}
	})

	t.Run("robots.txt down for a moment", func(t *testing.T) {
		// other.test: docs.test's robots.txt is already cached.
		h.site.set("other.test", "/later", htmlPage("Later", "ok()\n"))
		h.site.mu.Lock()
		h.site.robotsDown = 1
		h.site.mu.Unlock()
		imp := h.wait(h.ada, h.request(h.ada, h.site.url("other.test", "/later")).Import.ID)
		if imp.Status != store.ImportDone {
			t.Fatalf("import = %+v, want done once robots.txt answers again", imp)
		}
		if jobs := h.waitJobs(imp.ID); fmt.Sprint(jobs) != "[completed/2]" {
			t.Errorf("jobs = %v, want completed on the second attempt", jobs)
		}
	})

	t.Run("a transient error fails after three attempts", func(t *testing.T) {
		h.site.mu.Lock()
		h.site.retryAfter = ""
		h.site.mu.Unlock()
		h.site.set("docs.test", "/down", htmlPage("Down", "x\n"))
		h.site.failFirst("docs.test", "/down", http.StatusBadGateway, http.StatusBadGateway, http.StatusBadGateway)
		imp := h.wait(h.ada, h.request(h.ada, h.site.url("docs.test", "/down")).Import.ID)
		if imp.Status != store.ImportFailed || imp.ErrorCode != "upstream_error" {
			t.Fatalf("import = %+v, want failed with upstream_error", imp)
		}
		if jobs := h.waitJobs(imp.ID); fmt.Sprint(jobs) != "[discarded/3]" {
			t.Errorf("jobs = %v, want discarded after 3 attempts", jobs)
		}
	})
}

func TestPermanentErrorsFailAtOnce(t *testing.T) {
	h := newHarness(t, time.Hour)
	tests := []struct {
		path, code string
	}{
		{"/private/notes", "disallowed_by_robots"},
		{"/missing", "not_found"},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			imp := h.wait(h.ada, h.request(h.ada, h.site.url("docs.test", tt.path)).Import.ID)
			if imp.Status != store.ImportFailed || imp.ErrorCode != tt.code {
				t.Fatalf("import = %+v, want failed with %s", imp, tt.code)
			}
			if jobs := h.waitJobs(imp.ID); fmt.Sprint(jobs) != "[cancelled/1]" {
				t.Errorf("jobs = %v, want cancelled on the first attempt", jobs)
			}
		})
	}
}

func TestDuplicateJobsMakeOneDocument(t *testing.T) {
	h := newHarness(t, time.Hour)
	h.site.set("docs.test", "/guide", htmlPage("Guide", "x\n"))
	url := h.site.url("docs.test", "/guide")

	t.Run("the same job twice", func(t *testing.T) {
		req := h.request(h.ada, url)
		h.enqueue(Args{ImportID: req.Import.ID, OwnerID: h.ada.ID})
		imp := h.wait(h.ada, req.Import.ID)
		jobs := h.waitJobs(req.Import.ID)
		if imp.Status != store.ImportDone || h.count("documents") != 1 {
			t.Errorf("import = %+v with %d documents, want done with 1", imp, h.count("documents"))
		}
		if fmt.Sprint(jobs) != "[completed/1 completed/1]" {
			t.Errorf("jobs = %v, want both completed", jobs)
		}
	})

	t.Run("two imports of one page at once", func(t *testing.T) {
		h.site.set("docs.test", "/other", htmlPage("Other", "y\n"))
		other := h.site.url("docs.test", "/other")
		a := h.request(h.grace, other)
		b := h.request(h.grace, other+"#section")
		first, second := h.wait(h.grace, a.Import.ID), h.wait(h.grace, b.Import.ID)
		if first.Status != store.ImportDone || first.DocumentID != second.DocumentID {
			t.Errorf("imports = %+v and %+v, want both done with one document", first, second)
		}
		docs, err := h.store.ListDocuments(t.Context(), h.grace.ID, nil, 10)
		if err != nil || len(docs) != 1 {
			t.Errorf("Grace's documents = %d, %v; want 1", len(docs), err)
		}
	})
}

// A job delivered again after its import finished, as when the worker dies
// between the import's transaction and the job's completion, leaves the
// import exactly as it was.
func TestRedeliveryLeavesAFinishedImportAlone(t *testing.T) {
	h := newHarness(t, time.Hour)
	h.site.set("docs.test", "/guide", htmlPage("Guide", "x\n"))
	done := h.wait(h.ada, h.request(h.ada, h.site.url("docs.test", "/guide")).Import.ID)
	if done.Status != store.ImportDone {
		t.Fatalf("import = %+v, want done", done)
	}
	h.enqueue(Args{ImportID: done.ID, OwnerID: h.ada.ID})
	if jobs := h.waitJobs(done.ID); fmt.Sprint(jobs) != "[completed/1 completed/1]" {
		t.Fatalf("jobs = %v, want both completed", jobs)
	}
	again, err := h.store.GetImport(t.Context(), h.ada.ID, done.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !again.UpdatedAt.Equal(done.UpdatedAt) || again.Status != done.Status || again.DocumentID != done.DocumentID {
		t.Errorf("import after the redelivery = %+v, want it untouched: %+v", again, done)
	}
}

func TestCrossHostCanonicalIsIgnored(t *testing.T) {
	h := newHarness(t, time.Hour)
	victim := h.site.url("docs.test", "/guide")
	h.site.set("docs.test", "/guide", htmlPage("Real guide", "real()\n"))
	h.site.set("other.test", "/copy", strings.Replace(htmlPage("Fake guide", "fake()\n"),
		"<title>", `<link rel="canonical" href="`+victim+`"><title>`, 1))

	h.wait(h.ada, h.request(h.ada, h.site.url("other.test", "/copy")).Import.ID)
	imp := h.wait(h.grace, h.request(h.grace, victim).Import.ID)
	doc, err := h.store.GetDocument(t.Context(), h.grace.ID, imp.DocumentID)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Title != "Real guide" || doc.SourceURL != victim {
		t.Errorf("Grace's import of %s = %q from %s, want the real page", victim, doc.Title, doc.SourceURL)
	}
	if n := h.count("sources"); n != 2 {
		t.Errorf("sources = %d, want 2: the copy keeps its own key", n)
	}
}

func TestJobForAnotherOwnerIsCancelled(t *testing.T) {
	h := newHarness(t, time.Hour)
	h.site.set("docs.test", "/guide", htmlPage("Guide", "x\n"))
	// Stop the worker so Ada's own job cannot run first.
	h.stop()
	req := h.request(h.ada, h.site.url("docs.test", "/guide"))
	if _, err := testDB.Pool.Exec(t.Context(), "DELETE FROM river_job"); err != nil {
		t.Fatal(err)
	}
	h.enqueue(Args{ImportID: req.Import.ID, OwnerID: h.grace.ID})
	h.start()
	if jobs := h.waitJobs(req.Import.ID); fmt.Sprint(jobs) != "[cancelled/1]" {
		t.Errorf("jobs = %v, want the mismatched job cancelled", jobs)
	}
	imp, err := h.store.GetImport(t.Context(), h.ada.ID, req.Import.ID)
	if err != nil || imp.Status != store.ImportQueued {
		t.Errorf("Ada's import = %+v, %v; want it untouched", imp, err)
	}
	if hits := h.site.hitTimes("/guide"); len(hits) != 0 {
		t.Errorf("the page was fetched %d times for a job whose owner does not match", len(hits))
	}
}
