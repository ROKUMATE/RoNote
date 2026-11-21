//go:build integration

package s3blob_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/rokumate/ronote/internal/blob"
	"github.com/rokumate/ronote/internal/blob/blobtest"
	"github.com/rokumate/ronote/internal/blob/s3blob"
	"github.com/rokumate/ronote/internal/s3test"
)

func TestConformance(t *testing.T) {
	t.Parallel()
	cfg := s3test.New(t)
	blobtest.Run(t, s3blob.New(cfg))

	// The check at start and in check-config (docs/adr/0015-operations.md):
	// production's bucket is made by hand, so a wrong name must stop the
	// api and the worker, saying which.
	t.Run("a missing bucket fails the check, by name", func(t *testing.T) {
		cfg := cfg
		cfg.Bucket = "ronote-missing"
		err := s3blob.New(cfg).Check(t.Context())
		if err == nil || !strings.Contains(err.Error(), "bucket ronote-missing") {
			t.Errorf("Check on a missing bucket = %v, want an error naming it", err)
		}
	})
	t.Run("the bucket passes the check, which leaves nothing", func(t *testing.T) {
		store := s3blob.New(cfg)
		if err := store.Check(t.Context()); err != nil {
			t.Fatalf("Check = %v", err)
		}
		for info, err := range store.List(t.Context(), ".ronote-check/") {
			if err != nil {
				t.Fatal(err)
			}
			t.Errorf("the check left %s in the bucket", info.Key)
		}
	})

	t.Run("wrong secret", func(t *testing.T) {
		cfg := cfg
		cfg.SecretKey = "wrong"
		_, err := s3blob.New(cfg).Get(t.Context(), "any")
		if err == nil || errors.Is(err, blob.ErrNotFound) {
			t.Errorf("Get with a wrong secret = %v, want an access error", err)
		}
	})
}
