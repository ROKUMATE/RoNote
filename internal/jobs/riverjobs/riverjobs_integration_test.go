//go:build integration

package riverjobs

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/rokumate/ronote/internal/jobs/jobstest"
	"github.com/rokumate/ronote/internal/pgtest"
)

var testDB *pgtest.DB

func TestMain(m *testing.M) {
	pgtest.Main(m, &testDB)
}

const (
	retryDelay   = 100 * time.Millisecond
	pollInterval = 50 * time.Millisecond
)

func TestConformance(t *testing.T) {
	jobstest.Run(t, func(t *testing.T) jobstest.Adapter {
		testDB.Reset(t)
		q, err := New(testDB.Pool, Options{
			Logger:            slog.New(slog.DiscardHandler),
			RetryDelay:        func(int) time.Duration { return retryDelay },
			FetchCooldown:     20 * time.Millisecond,
			FetchPollInterval: pollInterval,
		})
		if err != nil {
			t.Fatal(err)
		}
		return jobstest.Adapter{
			Enqueuer:   q,
			Runner:     q,
			Begin:      func(ctx context.Context) (pgx.Tx, error) { return testDB.Pool.Begin(ctx) },
			Wait:       time.Sleep,
			Now:        time.Now,
			RetryDelay: retryDelay,
			Poll:       pollInterval,
			Settle:     time.Second,
		}
	})
}
