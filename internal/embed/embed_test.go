package embed

import (
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

func TestClientAgainstFakeServer(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(FakeServer())
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL+"/v1/", "qwen3-embedding:0.6b")
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
			_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[0.1,0.2]}]}`))
		}, rejected: true},
		{name: "too few vectors", handler: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"data":[]}`))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(tt.handler)
			t.Cleanup(srv.Close)
			_, err := NewClient(srv.URL, "m").Embed(t.Context(), []string{"x"})
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
	_, err := NewClient(url, "m").Embed(t.Context(), []string{"x"})
	if err == nil || errors.Is(err, ErrRejected) {
		t.Errorf("err = %v, want a transient error", err)
	}
}
