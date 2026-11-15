// Command fakedocling replays recorded answers of docling-serve and pdftext
// (docs/adr/0013-pdf-import.md) for dev and the end-to-end tests, like
// cmd/fakellm: the worker runs its real client and needs no docling image.
// It serves docling on RONOTE_FAKEDOCLING_ADDR and pdftext on
// RONOTE_FAKEPDFTEXT_ADDR, from the recordings in
// RONOTE_FAKEDOCLING_RECORDINGS (testdata/pdf), with the dev stack's keys
// unless RONOTE_DOCLING_API_KEY and RONOTE_PDFTEXT_API_KEY say otherwise.
package main

import (
	"fmt"
	"os"

	"github.com/rokumate/ronote/internal/devserver"
	"github.com/rokumate/ronote/internal/fakedocling"
)

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func main() {
	dir := env("RONOTE_FAKEDOCLING_RECORDINGS", "testdata/pdf")
	recs, err := fakedocling.Load(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakedocling:", err)
		os.Exit(1)
	}
	handler := fakedocling.Handler(fakedocling.Keys{
		Docling: env("RONOTE_DOCLING_API_KEY", "ronote-dev-docling"),
		PDFText: env("RONOTE_PDFTEXT_API_KEY", "ronote-dev-pdftext"),
	}, recs)
	// One handler answers both: each route checks its own service's key.
	errs := make(chan error, 2)
	go func() {
		errs <- devserver.Run("fakedocling", env("RONOTE_FAKEDOCLING_ADDR", "127.0.0.1:5001"), handler, "recordings", len(recs))
	}()
	go func() {
		errs <- devserver.Run("fakepdftext", env("RONOTE_FAKEPDFTEXT_ADDR", "127.0.0.1:5002"), handler)
	}()
	if err := <-errs; err != nil {
		fmt.Fprintln(os.Stderr, "fakedocling:", err)
		os.Exit(1)
	}
}
