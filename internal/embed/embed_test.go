package embed

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func dot(a, b []float32) float64 {
	var s float64
	for i := range a {
		s += float64(a[i]) * float64(b[i])
	}
	return s
}

// clientFor is a Client for model at url, through srv's own HTTP client.
func clientFor(srv *httptest.Server, url, model string) *Client {
	c := NewClient(url, model)
	c.HTTP = srv.Client()
	return c
}

func TestClientAgainstFakeServer(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(FakeServer())
	t.Cleanup(srv.Close)
	c := clientFor(srv, srv.URL+"/", "qwen3-embedding:0.6b")
	texts := make([]string, batchSize+5) // two requests
	for i := range texts {
		texts[i] = strings.Repeat("word ", i+1)
	}
	texts[0], texts[1] = "goroutines run concurrently", "concurrently run goroutines"

	got, err := c.Embed(t.Context(), texts)
	if err != nil {
		t.Fatal(err)
	}
	if c.Model() != "qwen3-embedding:0.6b" {
		t.Errorf("Model() = %q", c.Model())
	}
	if len(got) != len(texts) || len(got[0]) != Dims {
		t.Fatalf("%d vectors of %d, want %d of %d", len(got), len(got[0]), len(texts), Dims)
	}
	if n := math.Sqrt(dot(got[2], got[2])); math.Abs(n-1) > 1e-5 {
		t.Errorf("norm = %v, want 1", n)
	}
	if s := dot(got[0], got[1]); math.Abs(s-1) > 1e-5 {
		t.Errorf("the same words in another order score %v, want 1", s)
	}
	if s := dot(got[0], got[5]); s > 0.5 {
		t.Errorf("unrelated texts score %v, want little", s)
	}
}

// TestClientAsksOllamaNotToTruncate checks the request: Ollama's native
// /api/embed, with truncate false, so an input over the context fails
// instead of being cut and embedded as if whole.
func TestClientAsksOllamaNotToTruncate(t *testing.T) {
	t.Parallel()
	var got struct {
		Model    string   `json:"model"`
		Input    []string `json:"input"`
		Truncate *bool    `json:"truncate"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/embed" {
			http.Error(w, "not here", http.StatusNotFound)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		_, _ = w.Write([]byte(`{"embeddings":[` + vectorJSON(Dims) + `]}`))
	}))
	t.Cleanup(srv.Close)
	if _, err := clientFor(srv, srv.URL, "qwen3-embedding:0.6b").Embed(t.Context(), []string{"x"}); err != nil {
		t.Fatal(err)
	}
	if got.Model != "qwen3-embedding:0.6b" || len(got.Input) != 1 || got.Truncate == nil || *got.Truncate {
		t.Errorf("request = %+v, want the model, one input and truncate false", got)
	}
}

// vectorJSON is a JSON vector of n numbers.
func vectorJSON(n int) string {
	return "[" + strings.TrimSuffix(strings.Repeat("0.1,", n), ",") + "]"
}

func TestClientErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		handler  http.HandlerFunc
		rejected bool
	}{
		{name: "a 4xx is a rejection", handler: func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"error":"model not found"}`, http.StatusNotFound)
		}, rejected: true},
		{name: "a 5xx is worth retrying", handler: func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "overloaded", http.StatusServiceUnavailable)
		}},
		{name: "the wrong dimensions are a rejection", handler: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"embeddings":[[0.1,0.2]]}`))
		}, rejected: true},
		{name: "too few vectors", handler: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"embeddings":[]}`))
		}},
		// Ollama's answer to an input over the model's context, with
		// truncate false: refused for good, not cut and embedded.
		{name: "an input over the context is a rejection", handler: func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"error":"the input length exceeds the context length"}`, http.StatusBadRequest)
		}, rejected: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(tt.handler)
			t.Cleanup(srv.Close)
			_, err := clientFor(srv, srv.URL, "m").Embed(t.Context(), []string{"x"})
			if err == nil {
				t.Fatal("want an error")
			}
			if got := errors.Is(err, ErrRejected); got != tt.rejected {
				t.Errorf("rejected = %v (%v), want %v", got, err, tt.rejected)
			}
		})
	}
}

func TestClientUnreachable(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	_, err := clientFor(srv, url, "m").Embed(t.Context(), []string{"x"})
	if err == nil || errors.Is(err, ErrRejected) {
		t.Errorf("err = %v, want a transient error", err)
	}
}
