// Command fakeidp serves a fake GitHub sign-in for dev and end-to-end tests
// (docs/adr/0010-sign-in-and-sessions.md). It offers the users in the JSON
// file RONOTE_FAKEIDP_USERS and accepts the API's OAuth app,
// RONOTE_GITHUB_CLIENT_ID and RONOTE_GITHUB_CLIENT_SECRET.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rokumate/ronote/internal/fakeidp"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fakeidp:", err)
		os.Exit(1)
	}
}

func run() error {
	addr := withDefault(os.Getenv("RONOTE_FAKEIDP_ADDR"), "127.0.0.1:8090")
	usersFile := withDefault(os.Getenv("RONOTE_FAKEIDP_USERS"), "deploy/fakeidp/dev-users.json")
	clientID, secret := os.Getenv("RONOTE_GITHUB_CLIENT_ID"), os.Getenv("RONOTE_GITHUB_CLIENT_SECRET")
	if clientID == "" || secret == "" {
		return errors.New("RONOTE_GITHUB_CLIENT_ID and RONOTE_GITHUB_CLIENT_SECRET must be set, to the API's values")
	}
	users, err := fakeidp.LoadUsers(usersFile)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	srv := &http.Server{
		Handler:           fakeidp.New(fakeidp.Options{ClientID: clientID, ClientSecret: secret, Users: users}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	logger.Info("fakeidp listening", "addr", ln.Addr().String(), "users", len(users))

	select {
	case err := <-serveErr:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func withDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
