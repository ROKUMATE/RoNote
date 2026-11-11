package httpapi

import (
	"errors"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rokumate/ronote/internal/httpapi/gen"
	"github.com/rokumate/ronote/internal/ratelimit"
)

// Limits are the API's rate limits; zero means none
// (docs/adr/0010-sign-in-and-sessions.md).
type Limits struct {
	// APIPerMinute counts one user's requests, the import event stream
	// excepted.
	APIPerMinute int
	// AuthPerMinute counts one IP's requests to /api/v1/auth.
	AuthPerMinute int
}

// limiters count requests in this process.
type limiters struct {
	perUser, perIP *ratelimit.Limiter // nil for no limit
}

func newLimiters(l Limits) limiters {
	var lim limiters
	if l.APIPerMinute > 0 {
		lim.perUser = ratelimit.New(l.APIPerMinute, time.Minute, time.Now)
	}
	if l.AuthPerMinute > 0 {
		lim.perIP = ratelimit.New(l.AuthPerMinute, time.Minute, time.Now)
	}
	return lim
}

// byIP limits requests per client IP: the connection's, since no proxy's
// headers are trusted yet.
func (l limiters) byIP(next http.Handler) http.Handler {
	return limit(l.perIP, func(r *http.Request) string {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			return r.RemoteAddr
		}
		return host
	}, next)
}

// byUser limits a signed-in user's requests, the import event stream
// excepted: a browser holds one open per import it follows.
func (l limiters) byUser(next http.Handler) http.Handler {
	limited := limit(l.perUser, func(r *http.Request) string { return userFrom(r.Context()).String() }, next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/imports/") && strings.HasSuffix(r.URL.Path, "/events") {
			next.ServeHTTP(w, r)
			return
		}
		limited.ServeHTTP(w, r)
	})
}

func limit(l *ratelimit.Limiter, key func(*http.Request) string, next http.Handler) http.Handler {
	if l == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ok, wait := l.Allow(key(r)); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(retrySeconds(wait)))
			writeError(w, r, http.StatusTooManyRequests, gen.ErrorCodeRateLimited, "too many requests")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// retrySeconds is d in whole seconds for Retry-After: rounded up, at
// least 1.
func retrySeconds(d time.Duration) int {
	return max(1, int(math.Ceil(d.Seconds())))
}

// Request body limits.
const (
	maxBody         = 64 << 10
	maxDocumentBody = 8 << 20 // a whole document, from a page of up to 5 MB
	maxReanchorBody = 1 << 20 // up to 2000 highlights' new places
)

// bodyLimit is how large r's body may be.
func bodyLimit(r *http.Request) int64 {
	p := r.URL.Path
	switch {
	case r.Method == http.MethodPatch && strings.HasPrefix(p, "/api/v1/documents/") && strings.Count(p, "/") == 4:
		return maxDocumentBody
	case r.Method == http.MethodPost && strings.HasSuffix(p, "/annotations/reanchor"):
		return maxReanchorBody
	}
	return maxBody
}

// limitBody refuses a body larger than its route takes: at once when it
// says its length, and when reading it otherwise.
func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := bodyLimit(r)
		if r.ContentLength > limit {
			writeError(w, r, http.StatusRequestEntityTooLarge, gen.ErrorCodeBodyTooLarge, "the request body is too large")
			return
		}
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
		}
		next.ServeHTTP(w, r)
	})
}

// badBody answers a body that couldn't be read: 413 when it was too large,
// 400 with message otherwise.
func badBody(w http.ResponseWriter, r *http.Request, err error, message string) {
	if tooLarge := new(http.MaxBytesError); errors.As(err, &tooLarge) {
		writeError(w, r, http.StatusRequestEntityTooLarge, gen.ErrorCodeBodyTooLarge, "the request body is too large")
		return
	}
	writeError(w, r, http.StatusBadRequest, gen.ErrorCodeInvalidRequest, message)
}
