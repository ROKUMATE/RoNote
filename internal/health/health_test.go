package health

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBeatTouchesTheFileOnlyWhilePingSucceeds(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tests := []struct {
		name string
		ping func(context.Context) error
		want bool
	}{
		{name: "the database answers", ping: func(context.Context) error { return nil }, want: true},
		{name: "it doesn't", ping: func(context.Context) error { return errors.New("down") }, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(dir, tt.name)
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan struct{})
			go func() {
				Beat(ctx, path, time.Hour, func(ctx context.Context) error {
					defer cancel()
					return tt.ping(ctx)
				})
				close(done)
			}()
			<-done
			_, err := os.Stat(path)
			if got := err == nil; got != tt.want {
				t.Errorf("file written = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFresh(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "alive")
	now := time.Now()
	if err := Fresh(path, time.Minute, now); err == nil {
		t.Error("Fresh passed with no file")
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, now, now.Add(-30*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := Fresh(path, time.Minute, now); err != nil {
		t.Errorf("Fresh 30s after a beat: %v", err)
	}
	if err := Fresh(path, time.Minute, now.Add(time.Minute)); err == nil {
		t.Error("Fresh passed 90s after the last beat, with a minute allowed")
	}
}

func TestGet(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.Error(w, "down", http.StatusServiceUnavailable)
		}
	}))
	t.Cleanup(srv.Close)
	if err := Get(t.Context(), srv.URL+"/healthz", time.Second); err != nil {
		t.Errorf("Get /healthz: %v", err)
	}
	if err := Get(t.Context(), srv.URL+"/other", time.Second); err == nil {
		t.Error("Get passed a 503")
	}
	if err := Get(t.Context(), "http://127.0.0.1:1/healthz", time.Second); err == nil {
		t.Error("Get passed with nothing listening")
	}
}
