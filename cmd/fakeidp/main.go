// Command fakeidp serves a fake GitHub sign-in for dev and end-to-end tests
// (docs/adr/0010-sign-in-and-sessions.md). It offers the users in the JSON
// file RONOTE_FAKEIDP_USERS and accepts the API's OAuth app,
// RONOTE_GITHUB_CLIENT_ID and RONOTE_GITHUB_CLIENT_SECRET. With
// RONOTE_FAKEIDP_REGISTRATION=true it also adds users on request, for the
// end-to-end tests.
package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/rokumate/ronote/internal/devserver"
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
	// Registration lets the e2e suite mint a user per test; dev has no use
	// for it.
	registration, _ := strconv.ParseBool(os.Getenv("RONOTE_FAKEIDP_REGISTRATION"))
	handler := fakeidp.New(fakeidp.Options{ClientID: clientID, ClientSecret: secret, Users: users, Registration: registration})
	return devserver.Run("fakeidp", addr, handler, "users", len(users))
}

func withDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
