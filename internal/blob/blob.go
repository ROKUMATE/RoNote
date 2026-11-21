// Package blob is the port to file storage: PDFs live there, not in
// Postgres (docs/adr/0013-pdf-import.md). s3blob adapts any S3-compatible
// store and memblob keeps blobs in memory for unit tests; blobtest checks
// both behave alike.
package blob

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"iter"
	"strings"
	"time"
)

// ErrNotFound means no blob has the key.
var ErrNotFound = errors.New("blob: not found")

// Store holds blobs by key. Keys are slash-separated paths.
type Store interface {
	// Put stores size bytes from body under key, replacing any blob there.
	// body is seekable so an adapter can read it twice, as S3 signing does.
	Put(ctx context.Context, key string, body io.ReadSeeker, size int64) error
	// Get opens the blob under key, or returns ErrNotFound.
	Get(ctx context.Context, key string) (*Object, error)
	// Delete removes the blob under key. A missing blob is not an error, so
	// a retried delete succeeds.
	Delete(ctx context.Context, key string) error
	// List yields every blob whose key starts with prefix, in key order.
	List(ctx context.Context, prefix string) iter.Seq2[Info, error]
}

// Object is an open blob. The caller closes Body.
type Object struct {
	Body io.ReadCloser
	Size int64
}

// Info describes a stored blob.
type Info struct {
	Key      string
	Size     int64
	Modified time.Time
}

// DeletePrefix removes every blob whose key starts with prefix. Like Delete,
// it succeeds when there is nothing left to remove.
func DeletePrefix(ctx context.Context, s Store, prefix string) error {
	var keys []string
	for info, err := range s.List(ctx, prefix) {
		if err != nil {
			return err
		}
		keys = append(keys, info.Key)
	}
	for _, key := range keys {
		if err := s.Delete(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

// checkPrefix holds the objects CheckWritable writes, apart from any blob.
const checkPrefix = ".ronote-check/"

// CheckWritable writes a small object to s and deletes it, failing with
// the bucket's name when either is refused: a store that is missing, or
// takes no writes, fails at start rather than at a reader's first upload
// (docs/adr/0015-operations.md).
func CheckWritable(ctx context.Context, s Store, bucket string) error {
	key := checkPrefix + rand.Text()
	body := strings.NewReader("ok")
	if err := s.Put(ctx, key, body, body.Size()); err != nil {
		return fmt.Errorf("blob store: bucket %s takes no write: %w", bucket, err)
	}
	if err := s.Delete(ctx, key); err != nil {
		return fmt.Errorf("blob store: bucket %s keeps %s: %w", bucket, key, err)
	}
	return nil
}
