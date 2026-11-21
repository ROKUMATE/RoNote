package blob_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/rokumate/ronote/internal/blob"
	"github.com/rokumate/ronote/internal/blob/memblob"
)

// refusing is a store that takes no writes, as a missing bucket or a key
// without write access behaves.
type refusing struct{ blob.Store }

func (refusing) Put(context.Context, string, io.ReadSeeker, int64) error {
	return errors.New("NoSuchBucket")
}

func TestCheckWritable(t *testing.T) {
	t.Parallel()
	t.Run("a writable store passes, and keeps nothing", func(t *testing.T) {
		t.Parallel()
		store := memblob.New()
		if err := blob.CheckWritable(t.Context(), store, "ronote"); err != nil {
			t.Fatalf("CheckWritable = %v, want nil", err)
		}
		for info, err := range store.List(t.Context(), "") {
			if err != nil {
				t.Fatal(err)
			}
			t.Errorf("the check left %s behind", info.Key)
		}
	})
	t.Run("a store that takes no write fails, naming the bucket", func(t *testing.T) {
		t.Parallel()
		err := blob.CheckWritable(t.Context(), refusing{memblob.New()}, "ronote-pdfs")
		if err == nil || !strings.Contains(err.Error(), "ronote-pdfs") || !strings.Contains(err.Error(), "NoSuchBucket") {
			t.Errorf("CheckWritable = %v, want an error naming bucket ronote-pdfs and the store's reason", err)
		}
	})
}
