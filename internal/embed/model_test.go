package embed

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ollama serves /api/tags listing model with digest, or nothing for "".
func ollama(t *testing.T, model, digest string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		if model == "" {
			_, _ = w.Write([]byte(`{"models":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"models":[{"name":"other:1b","digest":"0000"},{"name":"` + model + `","digest":"` + digest + `"}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSameModel(t *testing.T) {
	t.Parallel()
	const model = "qwen3-embedding:0.6b"
	pinned := ollama(t, model, "ac6da0dfba84")
	twin := ollama(t, model, "ac6da0dfba84")
	other := ollama(t, model, "1234567890ab")
	without := ollama(t, "", "")
	down := httptest.NewServer(http.NotFoundHandler())
	down.Close()
	client := pinned.Client()

	tests := []struct {
		name           string
		a, b           string
		ok, notAnswers bool
		says           string
	}{
		{name: "one digest", a: pinned.URL, b: twin.URL, ok: true},
		{name: "two digests", a: pinned.URL, b: other.URL, says: "different models"},
		{name: "the model missing", a: pinned.URL, b: without.URL, says: "doesn't serve " + model},
		{name: "one not answering", a: pinned.URL, b: down.URL, notAnswers: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := SameModel(t.Context(), client, tt.a, tt.b, model)
			switch {
			case tt.ok && err != nil:
				t.Fatalf("SameModel() = %v, want nil", err)
			case tt.ok:
			case err == nil:
				t.Fatal("SameModel() = nil, want an error")
			case errors.Is(err, ErrNotAnswering) != tt.notAnswers:
				t.Errorf("SameModel() = %v; not answering %v, want %v", err, errors.Is(err, ErrNotAnswering), tt.notAnswers)
			case !strings.Contains(err.Error(), tt.says):
				t.Errorf("SameModel() = %v, want it to say %q", err, tt.says)
			}
		})
	}
}

// switchable serves /api/tags listing model with digest once on is true,
// and 503 before.
func switchable(t *testing.T, model, digest string, on *atomic.Bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if !on.Load() {
			http.Error(w, "starting", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"models":[{"name":"` + model + `","digest":"` + digest + `"}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestChecked(t *testing.T) {
	t.Parallel()
	const model = "qwen3-embedding:0.6b"
	pinned := ollama(t, model, "ac6da0dfba84")
	twin := ollama(t, model, "ac6da0dfba84")
	other := ollama(t, model, "1234567890ab")
	without := ollama(t, "", "")
	client := pinned.Client()

	// checked is a Checked over the fake embedder, logging into its buffer.
	checked := func(index, query string) (*Checked, *strings.Builder) {
		var log strings.Builder
		return NewChecked(Fake{}, client, index, query, model, slog.New(slog.NewTextHandler(&log, nil))), &log
	}

	t.Run("one address for both is not checked, only warmed", func(t *testing.T) {
		t.Parallel()
		c, _ := checked("http://127.0.0.1:9", "http://127.0.0.1:9")
		if err := c.Warm(t.Context()); err != nil {
			t.Fatalf("Warm() = %v", err)
		}
		if _, err := c.Embed(t.Context(), []string{"x"}); err != nil {
			t.Errorf("Embed() = %v, want vectors", err)
		}
	})
	t.Run("one digest embeds once warm", func(t *testing.T) {
		t.Parallel()
		c, _ := checked(pinned.URL, twin.URL)
		if err := c.Warm(t.Context()); err != nil {
			t.Fatalf("Warm() = %v", err)
		}
		if _, err := c.Embed(t.Context(), []string{"x"}); err != nil {
			t.Errorf("Embed() = %v, want vectors", err)
		}
	})
	for name, query := range map[string]string{"two digests": other.URL, "the model missing": without.URL} {
		t.Run(name+" turns the vector half off, logged once", func(t *testing.T) {
			t.Parallel()
			c, log := checked(pinned.URL, query)
			for range 2 {
				if _, err := c.Embed(t.Context(), []string{"x"}); !errors.Is(err, ErrModelMismatch) {
					t.Fatalf("Embed() = %v, want ErrModelMismatch", err)
				}
			}
			if n := strings.Count(log.String(), ModelMismatch); n != 1 {
				t.Errorf("logged %q %d times, want once: %s", ModelMismatch, n, log.String())
			}
			if !strings.Contains(log.String(), "level=ERROR") {
				t.Errorf("logged %s, want an error", log.String())
			}
		})
	}
	t.Run("one not answering yet: off until both answer, then on", func(t *testing.T) {
		t.Parallel()
		var on atomic.Bool
		late := switchable(t, model, "ac6da0dfba84", &on)
		c, log := checked(pinned.URL, late.URL)
		if _, err := c.Embed(t.Context(), []string{"x"}); !errors.Is(err, ErrUnchecked) {
			t.Fatalf("Embed() before it answers = %v, want ErrUnchecked", err)
		}
		on.Store(true)
		if err := c.Warm(t.Context()); err != nil {
			t.Fatalf("Warm() once it answers = %v", err)
		}
		if _, err := c.Embed(t.Context(), []string{"x"}); err != nil {
			t.Errorf("Embed() once it answers = %v, want vectors", err)
		}
		if strings.Contains(log.String(), ModelMismatch) {
			t.Errorf("logged a mismatch: %s", log.String())
		}
	})
	t.Run("the watch decides without a search", func(t *testing.T) {
		t.Parallel()
		c, log := checked(pinned.URL, other.URL)
		c.Watch(t.Context(), 10*time.Millisecond)
		if !strings.Contains(log.String(), ModelMismatch) {
			t.Errorf("the watch logged %q, want the mismatch", log.String())
		}
	})
	t.Run("a search never loads the model: nothing is sent until it is warm", func(t *testing.T) {
		t.Parallel()
		inner := &slow{}
		c := NewChecked(inner, client, pinned.URL, twin.URL, model, slog.New(slog.DiscardHandler))
		if _, err := c.Embed(t.Context(), []string{"x"}); !errors.Is(err, ErrCold) {
			t.Fatalf("Embed() before warming = %v, want ErrCold", err)
		}
		if n := inner.calls.Load(); n != 0 {
			t.Fatalf("a search before warming sent %d requests, want none", n)
		}
		if err := c.Warm(t.Context()); err != nil || inner.calls.Load() != 1 {
			t.Fatalf("Warm() = %v after %d requests, want one", err, inner.calls.Load())
		}
	})
	t.Run("a search that times out leaves it cold until warmed again", func(t *testing.T) {
		t.Parallel()
		inner := &slow{}
		c := NewChecked(inner, client, pinned.URL, twin.URL, model, slog.New(slog.DiscardHandler))
		if err := c.Warm(t.Context()); err != nil {
			t.Fatal(err)
		}
		inner.stall.Store(true)
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		defer cancel()
		if _, err := c.Embed(ctx, []string{"x"}); err == nil {
			t.Fatal("Embed() past its deadline = nil, want an error")
		}
		inner.stall.Store(false)
		before := inner.calls.Load()
		if _, err := c.Embed(t.Context(), []string{"x"}); !errors.Is(err, ErrCold) || inner.calls.Load() != before {
			t.Fatalf("Embed() after a timeout = %v with %d more requests, want ErrCold and none", err, inner.calls.Load()-before)
		}
		if err := c.Warm(t.Context()); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Embed(t.Context(), []string{"x"}); err != nil {
			t.Errorf("Embed() warmed again = %v, want vectors", err)
		}
	})
	t.Run("the watch warms it at start, and again once it goes cold", func(t *testing.T) {
		t.Parallel()
		inner := &slow{}
		c := NewChecked(inner, client, pinned.URL, twin.URL, model, slog.New(slog.DiscardHandler))
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go c.Watch(ctx, 5*time.Millisecond)
		embeds := func() bool {
			for range 200 {
				if _, err := c.Embed(t.Context(), []string{"x"}); err == nil {
					return true
				}
				time.Sleep(5 * time.Millisecond)
			}
			return false
		}
		if !embeds() {
			t.Fatal("the watch never warmed it")
		}
		inner.stall.Store(true)
		short, stop := context.WithTimeout(t.Context(), 20*time.Millisecond)
		defer stop()
		_, _ = c.Embed(short, []string{"x"})
		inner.stall.Store(false)
		if !embeds() {
			t.Error("the watch never warmed it again after it went cold")
		}
	})
}

// slow is the fake embedder, counting its calls, that stalls until its
// caller gives up while stall is set.
type slow struct {
	Fake
	calls atomic.Int32
	stall atomic.Bool
}

func (s *slow) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	s.calls.Add(1)
	if s.stall.Load() {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return s.Fake.Embed(ctx, texts)
}
