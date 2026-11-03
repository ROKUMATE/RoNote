// Package canon puts URLs in the canonical form that sources and imports
// are keyed by, so the same page is fetched, stored and found once.
package canon

import (
	"errors"
	"net"
	"net/url"
	"strings"
)

// MaxLen is the longest URL, in bytes, that RoNote accepts or stores.
const MaxLen = 2048

var (
	// ErrInvalid means the input is not an absolute URL with a host.
	ErrInvalid = errors.New("not an absolute URL with a host")
	// ErrScheme means the URL is not http or https.
	ErrScheme = errors.New("only http and https URLs are allowed")
	// ErrTooLong means the URL is longer than MaxLen bytes.
	ErrTooLong = errors.New("URL is longer than 2048 bytes")
)

// Normalize returns raw in canonical form:
//   - scheme and host lowercased, the default port (80, 443) dropped;
//   - the fragment dropped, and an empty path written as "/";
//   - utm_* , gclid and fbclid query parameters dropped, every other
//     parameter kept byte for byte and in order.
//
// The path is kept as written. URLs with user info are invalid.
func Normalize(raw string) (string, error) {
	if len(raw) > MaxLen {
		return "", ErrTooLong
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Opaque != "" || u.Host == "" || u.User != nil {
		return "", ErrInvalid
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", ErrScheme
	}
	host, port := strings.ToLower(u.Hostname()), u.Port()
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		port = ""
	}
	switch {
	case host == "":
		return "", ErrInvalid
	case port != "":
		u.Host = net.JoinHostPort(host, port)
	case strings.Contains(host, ":"):
		u.Host = "[" + host + "]"
	default:
		u.Host = host
	}
	u.Fragment, u.RawFragment = "", ""
	if u.Path == "" {
		u.Path, u.RawPath = "/", ""
	}
	u.RawQuery = withoutTracking(u.RawQuery)
	u.ForceQuery = false
	s := u.String()
	if len(s) > MaxLen {
		return "", ErrTooLong
	}
	return s, nil
}

// withoutTracking drops utm_*, gclid and fbclid parameters from a raw query
// and keeps the others exactly as written.
func withoutTracking(query string) string {
	if query == "" {
		return ""
	}
	var kept []string
	for _, param := range strings.Split(query, "&") {
		key, _, _ := strings.Cut(param, "=")
		key = strings.ToLower(key)
		if strings.HasPrefix(key, "utm_") || key == "gclid" || key == "fbclid" {
			continue
		}
		kept = append(kept, param)
	}
	return strings.Join(kept, "&")
}

// Resolve returns the canonical URL of a fetched page. The page's own
// <link rel=canonical> counts only when it is on the same host as the page's
// final URL: trusting a canonical on another host would let any page claim
// to be another site's and replace that site's cached copy for everyone.
// Otherwise, and when the canonical is missing or invalid, it is the final
// URL. Both are normalized.
func Resolve(finalURL, pageCanonical string) (string, error) {
	final, err := Normalize(finalURL)
	if err != nil {
		return "", err
	}
	canonical, err := Normalize(pageCanonical)
	if err != nil || host(canonical) != host(final) {
		return final, nil
	}
	return canonical, nil
}

// host returns the host and port of a normalized URL.
func host(normalized string) string {
	u, _ := url.Parse(normalized)
	return u.Host
}
