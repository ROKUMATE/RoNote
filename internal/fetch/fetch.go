// Package fetch downloads one HTML page from a user-supplied URL. Users decide
// where our server connects, so every connection passes an address guard at
// dial time (guard.go) and every limit is explicit.
package fetch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"time"

	"golang.org/x/net/html/charset"
)

const (
	userAgent      = "RoNoteBot/0.1 (+https://github.com/ROKUMATE/RoNote)"
	maxRedirects   = 5
	connectTimeout = 5 * time.Second
)

// Errors a fetch can fail with. Test with errors.Is.
var (
	ErrInvalidURL       = errors.New("invalid URL")
	ErrBlockedAddress   = errors.New("blocked address")
	ErrTooManyRedirects = errors.New("too many redirects")
	ErrTooLarge         = errors.New("response too large")
	ErrUnsupportedType  = errors.New("unsupported content type")
	ErrTimeout          = errors.New("fetch timed out")
)

// StatusError reports a response status that is neither 2xx nor a 304 the
// request asked for.
type StatusError struct {
	Code int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("unexpected status %d %s", e.Code, http.StatusText(e.Code))
}

// Resolver looks up the addresses of a host. *net.Resolver implements it.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// Options configure a Fetcher.
type Options struct {
	// AllowPrivateNetworks lets fetches reach loopback, private, link-local
	// and CGNAT addresses on any port, so tests and local docs servers work.
	// Config only allows it when RONOTE_ENV is dev or test.
	AllowPrivateNetworks bool
	// Resolver looks up host names; nil means net.DefaultResolver.
	Resolver Resolver
}

// Request is one page to fetch.
type Request struct {
	URL string
	// ETag and LastModified come from an earlier fetch of the same page. When
	// either is set the request is conditional, and an unchanged page comes
	// back as Result.NotModified.
	ETag         string
	LastModified string
}

// Result is a fetched page.
type Result struct {
	// NotModified is true when a conditional request found the page
	// unchanged; Body is then empty.
	NotModified bool
	// FinalURL is the URL after redirects.
	FinalURL    string
	ContentType string
	// Body is the page decoded to UTF-8.
	Body []byte
	// BodySHA256 is the hex SHA-256 of the body as the server sent it,
	// before charset decoding.
	BodySHA256   string
	ETag         string
	LastModified string
	FetchedAt    time.Time
}

// Fetcher downloads pages. It is safe for concurrent use.
type Fetcher struct {
	client       *http.Client
	resolver     Resolver
	guard        func(netip.AddrPort) error
	allowAnyPort bool
	maxBody      int64
	totalTimeout time.Duration
}

// New returns a Fetcher with production limits.
func New(opts Options) *Fetcher {
	f := &Fetcher{
		resolver:     opts.Resolver,
		guard:        addressGuard(opts.AllowPrivateNetworks),
		allowAnyPort: opts.AllowPrivateNetworks,
		maxBody:      5 << 20,
		totalTimeout: 20 * time.Second,
	}
	if f.resolver == nil {
		f.resolver = net.DefaultResolver
	}
	f.client = &http.Client{
		Transport: &http.Transport{
			Proxy:                 nil, // never let the environment route fetches elsewhere
			DialContext:           f.dial,
			TLSHandshakeTimeout:   connectTimeout,
			ResponseHeaderTimeout: 10 * time.Second,
			MaxIdleConnsPerHost:   2,
			IdleConnTimeout:       30 * time.Second,
			ForceAttemptHTTP2:     true,
		},
		CheckRedirect: f.checkRedirect,
	}
	return f
}

// Fetch downloads one HTML page. It follows up to 5 redirects, checking each
// against the same rules, and gives up after 20 seconds in total.
func (f *Fetcher) Fetch(ctx context.Context, r Request) (*Result, error) {
	u, err := url.Parse(r.URL)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidURL, err)
	}
	if err := f.checkURL(u); err != nil {
		return nil, err
	}

	fetchCtx, cancel := context.WithTimeout(ctx, f.totalTimeout)
	defer cancel()
	res, err := f.get(fetchCtx, u, r)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if isTimeout(err) {
			return nil, fmt.Errorf("%w: %w", ErrTimeout, err)
		}
		return nil, err
	}
	return res, nil
}

func (f *Fetcher) get(ctx context.Context, u *url.URL, r Request) (*Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidURL, err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9")
	if r.ETag != "" {
		req.Header.Set("If-None-Match", r.ETag)
	}
	if r.LastModified != "" {
		req.Header.Set("If-Modified-Since", r.LastModified)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }() // nothing useful to do if closing a read body fails

	res := &Result{
		FinalURL:     resp.Request.URL.String(),
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
		FetchedAt:    time.Now().UTC(),
	}
	conditional := r.ETag != "" || r.LastModified != ""
	if resp.StatusCode == http.StatusNotModified && conditional {
		res.NotModified = true
		return res, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &StatusError{Code: resp.StatusCode}
	}
	if resp.ContentLength > f.maxBody {
		return nil, fmt.Errorf("%w: %d bytes declared, limit %d", ErrTooLarge, resp.ContentLength, f.maxBody)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, f.maxBody+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > f.maxBody {
		return nil, fmt.Errorf("%w: more than %d bytes", ErrTooLarge, f.maxBody)
	}

	res.ContentType = resp.Header.Get("Content-Type")
	if res.ContentType == "" {
		res.ContentType = http.DetectContentType(raw)
	}
	mediaType, _, err := mime.ParseMediaType(res.ContentType)
	if err != nil || (mediaType != "text/html" && mediaType != "application/xhtml+xml") {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedType, res.ContentType)
	}

	decoded, err := charset.NewReader(bytes.NewReader(raw), res.ContentType)
	if err != nil {
		return nil, fmt.Errorf("decode charset: %w", err)
	}
	if res.Body, err = io.ReadAll(decoded); err != nil {
		return nil, fmt.Errorf("decode charset: %w", err)
	}
	sum := sha256.Sum256(raw)
	res.BodySHA256 = hex.EncodeToString(sum[:])
	return res, nil
}

func (f *Fetcher) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > maxRedirects {
		return fmt.Errorf("%w: more than %d", ErrTooManyRedirects, maxRedirects)
	}
	return f.checkURL(req.URL)
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout())
}
