// Package httpapi serves the RoNote HTTP API.
package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/rokumate/ronote/internal/auth"
	"github.com/rokumate/ronote/internal/httpapi/gen"
)

// readyTimeout bounds the database ping behind /readyz.
const readyTimeout = time.Second

// Pinger reports whether the database answers. *pgxpool.Pool implements it.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Options are what the API handler serves with.
type Options struct {
	// Logger logs requests and panics.
	Logger *slog.Logger
	// DB answers /readyz.
	DB Pinger
	// Backend serves /api/v1.
	Backend Backend
	// Events feeds the import event streams.
	Events Events
	// Done closes when the server begins to shut down, which ends event
	// streams so the shutdown need not wait for their clients.
	Done <-chan struct{}
	// Accounts keeps users and their sessions.
	Accounts Accounts
	// Cookies carry sessions and sign-ins in progress.
	Cookies auth.Cookies
	// GitHub is where people sign in.
	GitHub auth.GitHub
	// PublicURL is RoNote's origin, the one origin trusted to send unsafe
	// requests from another site.
	PublicURL string
	// Limits are the rate limits.
	Limits Limits
}

// New returns the API handler: /healthz, /readyz and the routes of
// api/openapi.yaml under /api/v1.
func New(o Options) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	mux.HandleFunc("GET /readyz", readyz(o.Logger, o.DB))
	protect := crossOrigin(o.PublicURL)
	lim := newLimiters(o.Limits)
	mux.Handle("GET /api/v1/auth/github/start", protect(lim.byIP(limitBody(signInStart(o)))))
	mux.Handle("GET /api/v1/auth/github/callback", protect(lim.byIP(limitBody(signInCallback(o)))))
	mux.Handle("/api/v1/", protect(requireSession(o, lim.byUser(limitBody(router{mux: apiRoutes(o, lim)})))))
	return withMiddleware(o.Logger, router{mux: mux})
}

// crossOrigin returns what wraps every /api route: Go's CSRF protection,
// which refuses unsafe requests a browser says came from another site, by
// Sec-Fetch-Site or, without it, by Origin against Host. publicURL is the
// only origin trusted besides; there are no bypass patterns
// (docs/adr/0010-sign-in-and-sessions.md).
func crossOrigin(publicURL string) func(http.Handler) http.Handler {
	cop := http.NewCrossOriginProtection()
	if publicURL != "" {
		if err := cop.AddTrustedOrigin(publicURL); err != nil {
			// Config checked it is an origin.
			panic(fmt.Sprintf("httpapi: trusted origin: %v", err))
		}
	}
	cop.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusForbidden, gen.ErrorCodeCrossOrigin, "cross-origin request refused")
	}))
	return cop.Handler
}

// router answers with the error envelope when the mux has no route for a
// request, and hands every other request to the mux.
type router struct {
	mux *http.ServeMux
}

func (rt router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fallback, pattern := rt.mux.Handler(r)
	if pattern != "" {
		rt.mux.ServeHTTP(w, r)
		return
	}

	// The mux's own fallback decides between 404 and 405 and sets Allow.
	rec := &headerRecorder{header: http.Header{}}
	fallback.ServeHTTP(rec, r)
	if rec.status == http.StatusMethodNotAllowed {
		w.Header().Set("Allow", rec.header.Get("Allow"))
		writeError(w, r, http.StatusMethodNotAllowed, gen.ErrorCodeMethodNotAllowed, "method not allowed")
		return
	}
	writeError(w, r, http.StatusNotFound, gen.ErrorCodeNotFound, "not found")
}

// headerRecorder keeps the status and headers of a response and drops its body.
type headerRecorder struct {
	header http.Header
	status int
}

func (r *headerRecorder) Header() http.Header { return r.header }

func (r *headerRecorder) Write(b []byte) (int, error) { return len(b), nil }

func (r *headerRecorder) WriteHeader(status int) { r.status = status }

type statusBody struct {
	Status string `json:"status"`
}

// healthz reports that the process is up. It checks no dependencies.
func healthz(w http.ResponseWriter, _ *http.Request) {
	writeOK(w)
}

// codeNotReady is the error code of /readyz, which shares the error envelope
// but is not part of the API spec.
const codeNotReady gen.ErrorCode = "not_ready"

// readyz reports whether the API can serve traffic: the database has to
// answer a ping within readyTimeout.
func readyz(logger *slog.Logger, db Pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
		defer cancel()
		if err := db.Ping(ctx); err != nil {
			logger.LogAttrs(ctx, slog.LevelWarn, "not ready",
				slog.String("error", err.Error()),
				slog.String("request_id", requestID(ctx)),
			)
			writeError(w, r, http.StatusServiceUnavailable, codeNotReady, "database unavailable")
			return
		}
		writeOK(w)
	}
}

func writeOK(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, statusBody{Status: "ok"})
}
