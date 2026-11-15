package memblob_test

import (
	"bytes"
	"testing"

	"github.com/rokumate/ronote/internal/blob/blobtest"
	"github.com/rokumate/ronote/internal/blob/memblob"
)

func TestConformance(t *testing.T) {
	t.Parallel()
	blobtest.Run(t, memblob.New())
}

func TestPutChecksSize(t *testing.T) {
	t.Parallel()
	s := memblob.New()
	for _, size := range []int64{2, 4} {
		if err := s.Put(t.Context(), "k", bytes.NewReader([]byte("abc")), size); err == nil {
			t.Errorf("Put of 3 bytes as %d succeeded", size)
		}
	}
}
