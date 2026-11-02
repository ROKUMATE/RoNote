package db

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"testing"
	"testing/synctest"
	"time"
)

func TestWaitUntil(t *testing.T) {
	t.Parallel()

	const ms = time.Millisecond
	errDown := errors.New("connection refused")

	tests := []struct {
		name     string
		failures int // pings that fail before one succeeds
		timeout  time.Duration
		wantErr  bool
		wantGaps []time.Duration // time between consecutive pings
	}{
		{name: "first ping succeeds", timeout: time.Second},
		{name: "succeeds on the third ping", failures: 2, timeout: time.Second, wantGaps: []time.Duration{100 * ms, 200 * ms}},
		{
			name: "gives up when the context ends", failures: 1000, timeout: time.Second, wantErr: true,
			wantGaps: []time.Duration{100 * ms, 200 * ms, 400 * ms},
		},
		{
			name: "delay doubles up to the cap", failures: 1000, timeout: 10 * time.Second, wantErr: true,
			wantGaps: []time.Duration{100 * ms, 200 * ms, 400 * ms, 800 * ms, 1600 * ms, 2000 * ms, 2000 * ms, 2000 * ms},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), tt.timeout)
				defer cancel()
				var pings []time.Time
				ping := func(context.Context) error {
					pings = append(pings, time.Now())
					if len(pings) <= tt.failures {
						return errDown
					}
					return nil
				}

				err := waitUntil(ctx, ping, slog.New(slog.DiscardHandler))

				if tt.wantErr != errors.Is(err, errDown) {
					t.Errorf("waitUntil() = %v, want error %v", err, tt.wantErr)
				}
				var gaps []time.Duration
				for i := 1; i < len(pings); i++ {
					gaps = append(gaps, pings[i].Sub(pings[i-1]))
				}
				if !slices.Equal(gaps, tt.wantGaps) {
					t.Errorf("gaps between pings = %v, want %v", gaps, tt.wantGaps)
				}
			})
		})
	}
}
