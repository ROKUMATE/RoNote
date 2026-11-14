// Package devserver runs the stand-ins dev and the end-to-end tests talk to
// instead of a real service: cmd/fakeidp, cmd/fakeembed and cmd/fakellm.
package devserver

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
)

// Run serves handler on addr until the process is interrupted or
// terminated, then shuts down within 5 seconds. It logs "<name> listening"
// with attrs once it accepts connections.
func Run(name, addr string, handler http.Handler, attrs ...any) error {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	logger.Info(name+" listening", append([]any{"addr", ln.Addr().String()}, attrs...)...)
	select {
	case err := <-serveErr:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
