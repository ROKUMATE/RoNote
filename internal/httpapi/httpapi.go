// Package httpapi serves the RoNote HTTP API.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
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
	// DevUser is the user every /api/v1 request acts as until real sign-in
	// exists; uuid.Nil answers 401. Config allows one in dev and test only.
	DevUser uuid.UUID
}

// New returns the API handler: /healthz, /readyz and the routes of
// api/openapi.yaml under /api/v1.
func New(o Options) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	mux.HandleFunc("GET /readyz", readyz(o.Logger, o.DB))
	mux.Handle("/api/v1/", requireUser(o.DevUser, router{mux: apiRoutes(o.Logger, o.Backend)}))
	return withMiddleware(o.Logger, router{mux: mux})
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
		writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	writeError(w, r, http.StatusNotFound, "not_found", "not found")
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
			writeError(w, r, http.StatusServiceUnavailable, "not_ready", "database unavailable")
			return
		}
		writeOK(w)
	}
}

func writeOK(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, statusBody{Status: "ok"})
}
