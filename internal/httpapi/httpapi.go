// Package httpapi serves the RoNote HTTP API.
package httpapi

import "net/http"

// New returns the API handler.
func New() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", ok)
	mux.HandleFunc("GET /readyz", ok)
	return router{mux: mux}
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
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	writeError(w, http.StatusNotFound, "not_found", "not found")
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

// ok reports that the API is up. /healthz and /readyz share it while
// readiness has no dependencies to check.
func ok(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, statusBody{Status: "ok"})
}
