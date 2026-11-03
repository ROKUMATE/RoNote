package importer

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/rokumate/ronote/internal/fetch"
)

// errExtract marks a page the extractor could not turn into valid IR.
var errExtract = errors.New("extraction failed")

// failure says what an error means for an import: the stable code it fails
// with, whether retrying could help, and how long the server asked to wait.
type failure struct {
	code       string
	permanent  bool
	retryAfter time.Duration
}

// classify sorts fetch and extract errors. Permanent errors fail the import
// at once; transient ones are retried, and give their code only if the last
// attempt fails too.
func classify(err error) failure {
	var status *fetch.StatusError
	switch {
	case errors.Is(err, fetch.ErrInvalidURL):
		return failure{code: "invalid_url", permanent: true}
	case errors.Is(err, fetch.ErrBlockedAddress):
		return failure{code: "blocked_address", permanent: true}
	case errors.Is(err, fetch.ErrDisallowedByRobots):
		return failure{code: "disallowed_by_robots", permanent: true}
	case errors.Is(err, fetch.ErrTooLarge):
		return failure{code: "too_large", permanent: true}
	case errors.Is(err, fetch.ErrUnsupportedType):
		return failure{code: "unsupported_type", permanent: true}
	case errors.Is(err, fetch.ErrTooManyRedirects):
		return failure{code: "too_many_redirects", permanent: true}
	case errors.Is(err, errExtract):
		return failure{code: "extract_failed", permanent: true}
	case errors.Is(err, fetch.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		return failure{code: "timeout"}
	case errors.As(err, &status):
		switch {
		case status.Code == http.StatusTooManyRequests:
			return failure{code: "rate_limited", retryAfter: status.RetryAfter}
		case status.Code >= 500:
			return failure{code: "upstream_error", retryAfter: status.RetryAfter}
		case status.Code == http.StatusNotFound, status.Code == http.StatusGone:
			return failure{code: "not_found", permanent: true}
		default:
			return failure{code: "http_error", permanent: true}
		}
	default:
		return failure{code: "network_error"}
	}
}
