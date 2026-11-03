package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/events"
	"github.com/rokumate/ronote/internal/httpapi/gen"
	"github.com/rokumate/ronote/internal/store"
)

// fakeFeed hands out one channel the test writes events to, and counts
// open subscriptions.
type fakeFeed struct {
	mu   sync.Mutex
	ch   chan events.Event
	subs int
}

func newFeed() *fakeFeed { return &fakeFeed{ch: make(chan events.Event, 16)} }

func (f *fakeFeed) Subscribe(uuid.UUID) (<-chan events.Event, func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subs++
	var once sync.Once
	return f.ch, func() {
		once.Do(func() {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.subs--
		})
	}
}

func (f *fakeFeed) open() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.subs
}

// streamWriter is a ResponseWriter that can be read while a handler writes.
type streamWriter struct {
	mu     sync.Mutex
	header http.Header
	status int
	body   strings.Builder
}

func (w *streamWriter) Header() http.Header { return w.header }

func (w *streamWriter) WriteHeader(status int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.status = status
}

func (w *streamWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.body.Write(b)
}

func (w *streamWriter) Flush() {}

func (w *streamWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.body.String()
}

// stream runs the event handler for import id in the background and
// returns its writer, a func that disconnects the client, and a channel
// closed when the handler returns.
func stream(t *testing.T, b Backend, feed Events, done <-chan struct{}, id uuid.UUID) (*streamWriter, context.CancelFunc, chan struct{}) {
	t.Helper()
	ctx, disconnect := context.WithCancel(context.WithValue(t.Context(), userKey{}, devUser))
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/imports/"+id.String()+"/events", http.NoBody)
	req.SetPathValue("id", id.String())
	w := &streamWriter{header: http.Header{}}
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		importEvents(b, feed, done, heartbeat).ServeHTTP(w, req)
	}()
	return w, disconnect, finished
}

const (
	evQueued     = "event: status\ndata: {\"status\":\"queued\"}\n\n"
	evFetching   = "event: status\ndata: {\"status\":\"fetching\"}\n\n"
	evExtracting = "event: status\ndata: {\"status\":\"extracting\"}\n\n"
)

func queuedImport() store.Import {
	return store.Import{ID: uuid.New(), OwnerID: devUser, Status: store.ImportQueued}
}

func TestStreamSendsChangesInOrderUntilDone(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		imp := queuedImport()
		feed := newFeed()
		doc := uuid.New()
		w, _, finished := stream(t, &fakeBackend{imports: map[uuid.UUID]store.Import{imp.ID: imp}}, feed, nil, imp.ID)
		synctest.Wait()

		for _, ev := range []events.Event{
			{ImportID: imp.ID, OwnerID: devUser, Status: "fetching"},
			{ImportID: imp.ID, OwnerID: devUser, Status: "fetching"},   // repeated: not sent again
			{ImportID: imp.ID, OwnerID: uuid.New(), Status: "failed"},  // someone else's: ignored
			{ImportID: imp.ID, OwnerID: devUser, Status: "extracting"}, //
			{ImportID: imp.ID, OwnerID: devUser, Status: "queued"},     // backwards: not sent
			{ImportID: imp.ID, OwnerID: devUser, Status: "done", DocumentID: doc},
		} {
			feed.ch <- ev
		}
		<-finished

		want := evQueued + evFetching + evExtracting +
			"event: status\ndata: {\"document_id\":\"" + doc.String() + "\",\"status\":\"done\"}\n\n"
		if got := w.String(); got != want {
			t.Errorf("stream =\n%s\nwant\n%s", got, want)
		}
		if w.status != http.StatusOK || w.header.Get("Content-Type") != "text/event-stream" || w.header.Get("Cache-Control") != "no-cache" {
			t.Errorf("status %d, headers %v", w.status, w.header)
		}
		if n := feed.open(); n != 0 {
			t.Errorf("subscriptions after the stream ended = %d, want 0", n)
		}
	})
}

func TestStreamOfAFinishedImportEndsAtOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		imp := queuedImport()
		imp.Status, imp.ErrorCode = store.ImportFailed, "not_found"
		feed := newFeed()
		w, _, finished := stream(t, &fakeBackend{imports: map[uuid.UUID]store.Import{imp.ID: imp}}, feed, nil, imp.ID)
		<-finished
		if got := w.String(); got != "event: status\ndata: {\"error_code\":\"not_found\",\"status\":\"failed\"}\n\n" {
			t.Errorf("stream = %q", got)
		}
		if n := feed.open(); n != 0 {
			t.Errorf("subscriptions = %d, want 0", n)
		}
	})
}

func TestStreamHeartbeatAndDisconnect(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		imp := queuedImport()
		feed := newFeed()
		w, disconnect, finished := stream(t, &fakeBackend{imports: map[uuid.UUID]store.Import{imp.ID: imp}}, feed, nil, imp.ID)

		time.Sleep(heartbeat - time.Second)
		synctest.Wait()
		if got := w.String(); got != evQueued {
			t.Errorf("before 15s the stream = %q, want only the status", got)
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if got := w.String(); got != evQueued+": heartbeat\n\n" {
			t.Errorf("after 15s the stream = %q, want a heartbeat", got)
		}

		disconnect()
		<-finished
		if n := feed.open(); n != 0 {
			t.Errorf("subscriptions after the client left = %d, want 0", n)
		}
	})
}

func TestStreamResyncsAndStopsOnShutdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		imp := queuedImport()
		b := &fakeBackend{imports: map[uuid.UUID]store.Import{imp.ID: imp}}
		feed := newFeed()
		done := make(chan struct{})
		w, _, finished := stream(t, b, feed, done, imp.ID)
		synctest.Wait()

		// The listener reconnected: the stream reads the import again.
		changed := imp
		changed.Status = store.ImportFetching
		b.imports[imp.ID] = changed
		feed.ch <- events.Event{ImportID: imp.ID, Resync: true}
		synctest.Wait()
		if got := w.String(); got != evQueued+evFetching {
			t.Errorf("after a resync the stream = %q", got)
		}

		close(done)
		<-finished
		if n := feed.open(); n != 0 {
			t.Errorf("subscriptions after shutdown = %d, want 0", n)
		}
	})
}

func TestStreamRefusals(t *testing.T) {
	t.Parallel()
	imp := queuedImport()
	feed := newFeed()
	b := &fakeBackend{imports: map[uuid.UUID]store.Import{imp.ID: imp}}
	h := New(Options{Logger: discardLogger, DB: dbUp, Backend: b, Events: feed, DevUser: devUser})
	for target, want := range map[string]gen.ErrorCode{
		"/api/v1/imports/" + uuid.NewString() + "/events": "not_found",
		"/api/v1/imports/not-a-uuid/events":               "invalid_parameter",
	} {
		if rec := apiRequest(t, h, http.MethodGet, target, ""); decodeError(t, rec).Code != want {
			t.Errorf("GET %s = %d %s, want %s", target, rec.Code, rec.Body, want)
		}
	}
	anonymous := New(Options{Logger: discardLogger, DB: dbUp, Backend: b, Events: feed})
	if rec := apiRequest(t, anonymous, http.MethodGet, "/api/v1/imports/"+imp.ID.String()+"/events", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("a stream without a user = %d, want 401", rec.Code)
	}
	if n := feed.open(); n != 0 {
		t.Errorf("subscriptions after refused streams = %d, want 0", n)
	}
}
