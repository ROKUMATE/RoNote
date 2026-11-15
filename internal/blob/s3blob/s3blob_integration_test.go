//go:build integration

package s3blob_test

import (
	"errors"
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

	t.Run("wrong secret", func(t *testing.T) {
		cfg := cfg
		cfg.SecretKey = "wrong"
		_, err := s3blob.New(cfg).Get(t.Context(), "any")
		if err == nil || errors.Is(err, blob.ErrNotFound) {
			t.Errorf("Get with a wrong secret = %v, want an access error", err)
		}
	})
}
