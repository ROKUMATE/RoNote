// Command fakeembed serves a deterministic embedder over Ollama's
// /api/embed, for dev and the end-to-end tests
// (docs/adr/0011-retrieval.md), like cmd/fakeidp: they use the real client
// and need no model. It listens on RONOTE_FAKEEMBED_ADDR.
package main

import (
	"fmt"
	"os"

	"github.com/rokumate/ronote/internal/devserver"
	"github.com/rokumate/ronote/internal/embed"
)

func main() {
	addr := os.Getenv("RONOTE_FAKEEMBED_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8093"
	}
	if err := devserver.Run("fakeembed", addr, embed.FakeServer()); err != nil {
		fmt.Fprintln(os.Stderr, "fakeembed:", err)
		os.Exit(1)
	}
}
