// Command fakellm serves a deterministic stand-in for Anthropic's Messages
// API, for dev and the end-to-end tests (docs/adr/0012-chat.md), like
// cmd/fakeembed: they use the real adapter and need no model. It listens on
// RONOTE_FAKELLM_ADDR.
package main

import (
	"fmt"
	"os"

	"github.com/rokumate/ronote/internal/devserver"
	"github.com/rokumate/ronote/internal/fakellm"
)

func main() {
	addr := os.Getenv("RONOTE_FAKELLM_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8094"
	}
	if err := devserver.Run("fakellm", addr, fakellm.Handler()); err != nil {
		fmt.Fprintln(os.Stderr, "fakellm:", err)
		os.Exit(1)
	}
}
