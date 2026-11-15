// Package blobtest checks that a blob.Store behaves as the port promises,
// so every adapter is held to the same contract.
package blobtest

import (
	"bytes"
	"errors"
	"io"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/blob"
)

// Run checks s. Each run works under its own random prefix, so stores can be
// shared between tests.
func Run(t *testing.T, s blob.Store) {
	t.Helper()
	root := "blobtest/" + uuid.NewString() + "/"

	put := func(t *testing.T, key, data string) {
		t.Helper()
		if err := s.Put(t.Context(), key, bytes.NewReader([]byte(data)), int64(len(data))); err != nil {
			t.Fatalf("Put(%s): %v", key, err)
		}
	}
	keys := func(t *testing.T, prefix string) []string {
		t.Helper()
		var got []string
		for info, err := range s.List(t.Context(), prefix) {
			if err != nil {
				t.Fatalf("List(%s): %v", prefix, err)
			}
			got = append(got, info.Key)
		}
		return got
	}
	missing := func(t *testing.T, key string) {
		t.Helper()
		if _, err := s.Get(t.Context(), key); !errors.Is(err, blob.ErrNotFound) {
			t.Errorf("Get(%s) = %v, want ErrNotFound", key, err)
		}
	}

	t.Run("round trip", func(t *testing.T) {
		key := root + "round/a.pdf"
		data := "%PDF-1.7\n\x00\xff binary"
		put(t, key, data)
		obj, err := s.Get(t.Context(), key)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = obj.Body.Close() }()
		got, err := io.ReadAll(obj.Body)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != data || obj.Size != int64(len(data)) {
			t.Errorf("Get = %q (size %d), want %q (size %d)", got, obj.Size, data, len(data))
		}
	})

	t.Run("put replaces", func(t *testing.T) {
		key := root + "replace/a"
		put(t, key, "first")
		put(t, key, "second")
		obj, err := s.Get(t.Context(), key)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = obj.Body.Close() }()
		if got, _ := io.ReadAll(obj.Body); string(got) != "second" {
			t.Errorf("Get = %q, want second", got)
		}
	})

	t.Run("missing key", func(t *testing.T) {
		missing(t, root+"missing/none")
	})

	t.Run("delete is idempotent", func(t *testing.T) {
		key := root + "delete/a"
		put(t, key, "x")
		for range 2 {
			if err := s.Delete(t.Context(), key); err != nil {
				t.Fatalf("Delete: %v", err)
			}
		}
		missing(t, key)
	})

	t.Run("list", func(t *testing.T) {
		start := time.Now()
		for _, k := range []string{"list/b/2", "list/a", "list/b/1", "listx/c"} {
			put(t, root+k, k)
		}
		want := []string{root + "list/a", root + "list/b/1", root + "list/b/2"}
		if got := keys(t, root+"list/"); !slices.Equal(got, want) {
			t.Errorf("List = %v, want %v", got, want)
		}
		for info, err := range s.List(t.Context(), root+"list/b/1") {
			if err != nil {
				t.Fatal(err)
			}
			if info.Size != int64(len("list/b/1")) {
				t.Errorf("size = %d, want %d", info.Size, len("list/b/1"))
			}
			// Stores keep times to the second, and their clock may differ
			// from the test's by a little.
			if d := info.Modified.Sub(start); d < -time.Minute || d > time.Minute {
				t.Errorf("modified %v, %v from the put", info.Modified, d)
			}
		}
		if got := keys(t, root+"nothing/"); len(got) != 0 {
			t.Errorf("List(empty prefix) = %v", got)
		}
	})

	t.Run("delete prefix", func(t *testing.T) {
		for _, k := range []string{"gone/a", "gone/b/c", "gone2/kept"} {
			put(t, root+k, k)
		}
		for range 2 {
			if err := blob.DeletePrefix(t.Context(), s, root+"gone/"); err != nil {
				t.Fatalf("DeletePrefix: %v", err)
			}
		}
		if got := keys(t, root+"gone/"); len(got) != 0 {
			t.Errorf("after DeletePrefix, List = %v", got)
		}
		if got := keys(t, root+"gone2/"); !slices.Equal(got, []string{root + "gone2/kept"}) {
			t.Errorf("DeletePrefix removed a sibling prefix: %v", got)
		}
	})
}
