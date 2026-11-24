package main

import (
	"bytes"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rokumate/ronote/internal/docling"
	"github.com/rokumate/ronote/internal/fakedocling"
)

// TestConvertCheck replays recorded fixtures through convert-check: one
// docling converts, and one it gave up on at its document timeout.
func TestConvertCheck(t *testing.T) {
	t.Parallel()
	fixtures := filepath.Join("..", "..", "testdata", "pdf")
	recs, err := fakedocling.Load(fixtures)
	if err != nil {
		t.Fatal(err)
	}
	keys := fakedocling.Keys{Docling: "d", PDFText: "p"}
	srv := httptest.NewServer(fakedocling.Handler(keys, recs))
	t.Cleanup(srv.Close)
	c := &docling.Client{
		Docling: docling.Service{URL: srv.URL, Key: keys.Docling}, PDFText: docling.Service{URL: srv.URL, Key: keys.PDFText},
		HTTP: srv.Client(), PollEvery: time.Millisecond,
	}

	tests := []struct {
		name, pdf, want, code string
	}{
		{name: "a PDF docling converts", pdf: "real-python-controlflow.pdf", want: "convert-check: ok\n"},
		{name: "a PDF docling gives up on", pdf: "timeout.pdf", code: docling.CodeTimeout},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pdf, err := os.ReadFile(filepath.Join(fixtures, tt.pdf)) //nolint:gosec // a fixture the test names
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			err = convertCheck(t.Context(), c, bytes.NewReader(pdf), &out)
			var refused *docling.RefusedError
			switch {
			case tt.code == "" && err != nil:
				t.Fatalf("convertCheck() = %v, want ok", err)
			case tt.code != "" && (!errors.As(err, &refused) || refused.Code != tt.code):
				t.Fatalf("convertCheck() = %v, want refused with %s", err, tt.code)
			case out.String() != tt.want:
				t.Errorf("printed %q, want %q", out.String(), tt.want)
			}
		})
	}

	t.Run("a PDF over the upload limit is not sent", func(t *testing.T) {
		t.Parallel()
		big := bytes.Repeat([]byte("x"), 25<<20+1)
		if err := convertCheck(t.Context(), c, bytes.NewReader(big), &bytes.Buffer{}); err == nil {
			t.Fatal("convertCheck() of 25 MiB and a byte = nil, want an error")
		}
	})
}
