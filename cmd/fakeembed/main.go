// Command fakeembed serves a deterministic embedder over OpenAI's
// /v1/embeddings, for dev and the end-to-end tests
// (docs/adr/0011-retrieval.md), like cmd/fakeidp: they use the real client
// and need no model. It listens on RONOTE_FAKEEMBED_ADDR.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rokumate/ronote/internal/embed"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fakeembed:", err)
		os.Exit(1)
	}
}

func run() error {
	addr := os.Getenv("RONOTE_FAKEEMBED_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8093"
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	srv := &http.Server{Handler: embed.FakeServer(), ReadHeaderTimeout: 5 * time.Second}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	logger.Info("fakeembed listening", "addr", ln.Addr().String())
	select {
	case err := <-serveErr:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
