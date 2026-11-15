// Package memblob is an in-memory blob.Store for unit tests.
package memblob

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"iter"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rokumate/ronote/internal/blob"
)

// Store keeps blobs in a map. The zero value is not usable; call New.
type Store struct {
	mu    sync.Mutex
	blobs map[string]stored
}

type stored struct {
	data     []byte
	modified time.Time
}

// New returns an empty store.
func New() *Store {
	return &Store{blobs: map[string]stored{}}
}

// Put stores a copy of body's size bytes.
func (s *Store) Put(_ context.Context, key string, body io.ReadSeeker, size int64) error {
	data, err := io.ReadAll(io.LimitReader(body, size+1))
	if err != nil {
		return err
	}
	if int64(len(data)) != size {
		return fmt.Errorf("memblob: put %s: body has %d bytes, want %d", key, len(data), size)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.blobs[key] = stored{data: data, modified: time.Now()}
	return nil
}

// Get returns a reader over the blob's bytes.
func (s *Store) Get(_ context.Context, key string) (*blob.Object, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.blobs[key]
	if !ok {
		return nil, blob.ErrNotFound
	}
	return &blob.Object{Body: io.NopCloser(bytes.NewReader(b.data)), Size: int64(len(b.data))}, nil
}

// Delete removes the blob, if there is one.
func (s *Store) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.blobs, key)
	return nil
}

// List yields the blobs under prefix as they were when it started.
func (s *Store) List(_ context.Context, prefix string) iter.Seq2[blob.Info, error] {
	s.mu.Lock()
	var infos []blob.Info
	for key, b := range s.blobs {
		if strings.HasPrefix(key, prefix) {
			infos = append(infos, blob.Info{Key: key, Size: int64(len(b.data)), Modified: b.modified})
		}
	}
	s.mu.Unlock()
	slices.SortFunc(infos, func(a, b blob.Info) int { return strings.Compare(a.Key, b.Key) })
	return func(yield func(blob.Info, error) bool) {
		for _, info := range infos {
			if !yield(info, nil) {
				return
			}
		}
	}
}

// Age makes every blob d older, for tests of what happens to old blobs.
func (s *Store) Age(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, b := range s.blobs {
		b.modified = b.modified.Add(-d)
		s.blobs[key] = b
	}
}
