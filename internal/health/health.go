// Package health is what the container health checks run
// (docs/adr/0014-production-topology.md): a GET of the api's /healthz, and,
// for the worker, which serves no HTTP, a file it touches while its
// database answers.
package health

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// Beat touches path now and every interval after, each time ping succeeds,
// until ctx ends. A ping that fails leaves the file as it was, so it goes
// stale and Fresh fails.
func Beat(ctx context.Context, path string, interval time.Duration, ping func(context.Context) error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		pingCtx, cancel := context.WithTimeout(ctx, interval/2)
		if ping(pingCtx) == nil {
			// A failed write shows as a stale file, which is what it means.
			_ = os.WriteFile(path, nil, 0o600)
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Fresh fails unless path was touched within maxAge before now.
func Fresh(path string, maxAge time.Duration, now time.Time) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("no heartbeat: %w", err)
	}
	if age := now.Sub(info.ModTime()); age > maxAge {
		return fmt.Errorf("last heartbeat %s ago, more than %s", age.Round(time.Second), maxAge)
	}
	return nil
}

// Get fails unless url answers 200 within timeout.
func Get(ctx context.Context, url string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
	if err := resp.Body.Close(); err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return errors.New(resp.Status)
	}
	return nil
}
