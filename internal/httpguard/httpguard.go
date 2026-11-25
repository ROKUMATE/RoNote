// Package httpguard keeps tests off net/http's shared default transport.
// httptest.Server.Close closes http.DefaultTransport's idle connections, so
// a parallel test whose request goes through it can find its connection
// closed under it ("http: CloseIdleConnections called"): a flake one run in
// hundreds. A package whose tests start servers calls Off from its
// TestMain, and every request through the default fails at once, naming
// the cause, so a test that forgot its own client fails every run.
package httpguard

import (
	"errors"
	"net/http"
)

// ErrSharedTransport is what a request through http.DefaultTransport gets.
var ErrSharedTransport = errors.New("httpguard: a test sent a request through http.DefaultTransport; give it a client of its own (a server's Client(), or one with its own *http.Transport)")

// Off makes http.DefaultTransport, and with it http.DefaultClient, refuse
// every request.
func Off() {
	http.DefaultTransport = refuse{}
}

type refuse struct{}

func (refuse) RoundTrip(*http.Request) (*http.Response, error) { return nil, ErrSharedTransport }
