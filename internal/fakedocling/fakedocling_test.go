package fakedocling_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rokumate/ronote/internal/docling"
	"github.com/rokumate/ronote/internal/fakedocling"
)

var record = flag.Bool("record", false, "record docling's and pdftext's answers for every fixture in testdata/pdf (make pdf-record)")

var fixtures = filepath.Join("..", "..", "testdata", "pdf")

// outcomes are what each hostile fixture comes to; every other fixture
// converts. not-a-pdf.pdf never reaches docling: the API refuses it.
var outcomes = map[string]string{
	"encrypted.pdf":  docling.CodePasswordProtected,
	"image-only.pdf": docling.CodeScanned,
	"pages-301.pdf":  docling.CodeTooManyPages,
	"timeout.pdf":    "unavailable",
}

// pdfs are the fixtures docling and pdftext see.
func pdfs(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(fixtures, "*.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, p := range paths {
		if filepath.Base(p) != "not-a-pdf.pdf" {
			out = append(out, p)
		}
	}
	if len(out) < 10 {
		t.Fatalf("found %d PDF fixtures, want the printed pages, the real PDFs and the hostile ones", len(out))
	}
	return out
}

// convert takes a PDF through what the worker calls, inspection to lines,
// and says how it ended: "" when it converted, else its error code, or
// "unavailable".
func convert(ctx context.Context, c *docling.Client, pdf []byte, name string) string {
	if _, err := c.Inspect(ctx, pdf); err != nil {
		return outcome(err)
	}
	doc, err := c.Convert(ctx, pdf, name)
	if err != nil {
		return outcome(err)
	}
	if _, err := c.Lines(ctx, pdf, docling.LayerBoxes(doc)); err != nil {
		return outcome(err)
	}
	return ""
}

func outcome(err error) string {
	var refused *docling.RefusedError
	switch {
	case errors.As(err, &refused):
		return refused.Code
	case errors.Is(err, docling.ErrUnavailable):
		return "unavailable"
	}
	return err.Error()
}

// TestRecordings replays every fixture's recording through the client the
// worker uses, and checks each ends as it should. A fixture printed again
// without being recorded again has no recording and fails here. With
// -record, it first records them from the real docling and pdftext.
func TestRecordings(t *testing.T) {
	if *record {
		recordAll(t)
	}
	recs, err := fakedocling.Load(fixtures)
	if err != nil {
		t.Fatal(err)
	}
	keys := fakedocling.Keys{Docling: "d", PDFText: "p"}
	srv := httptest.NewServer(fakedocling.Handler(keys, recs))
	t.Cleanup(srv.Close)
	c := &docling.Client{
		Docling: docling.Service{URL: srv.URL, Key: keys.Docling}, PDFText: docling.Service{URL: srv.URL, Key: keys.PDFText},
		HTTP: srv.Client(), PollEvery: time.Millisecond,
	}
	for _, path := range pdfs(t) {
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			pdf, err := os.ReadFile(path) //nolint:gosec // a fixture path the test chose
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := recs[sha(pdf)]; !ok {
				t.Fatalf("no recording of %s; run make pdf-record", name)
			}
			if got := convert(t.Context(), c, pdf, name); got != outcomes[name] {
				t.Errorf("replayed %s ends with %q, want %q", name, got, outcomes[name])
			}
		})
	}

	t.Run("a PDF never recorded", func(t *testing.T) {
		if got := convert(t.Context(), c, []byte("%PDF-1.7 unknown"), "x.pdf"); got != "unavailable" {
			t.Errorf("an unknown PDF ends with %q, want unavailable", got)
		}
	})
	t.Run("the wrong key", func(t *testing.T) {
		wrong := *c
		wrong.PDFText.Key = "x"
		if got := convert(t.Context(), &wrong, []byte("%PDF-"), "x.pdf"); got != "unavailable" {
			t.Errorf("with the wrong key: %q, want refused as unavailable", got)
		}
	})
}

// recordAll records every fixture from the docling and pdftext that
// RONOTE_DOCLING_URL and RONOTE_PDFTEXT_URL name (make dev-up-docling).
func recordAll(t *testing.T) {
	t.Helper()
	env := func(name, fallback string) string {
		if v := os.Getenv(name); v != "" {
			return v
		}
		return fallback
	}
	capture := &capture{answers: map[string]*fakedocling.Answer{}}
	c := &docling.Client{
		Docling:   docling.Service{URL: env("RONOTE_DOCLING_URL", "http://127.0.0.1:5001"), Key: env("RONOTE_DOCLING_API_KEY", "ronote-dev-docling")},
		PDFText:   docling.Service{URL: env("RONOTE_PDFTEXT_URL", "http://127.0.0.1:5002"), Key: env("RONOTE_PDFTEXT_API_KEY", "ronote-dev-pdftext")},
		HTTP:      &http.Client{Transport: capture, Timeout: 10 * time.Minute},
		PollEvery: 500 * time.Millisecond,
	}
	for _, path := range pdfs(t) {
		name := filepath.Base(path)
		pdf, err := os.ReadFile(path) //nolint:gosec // a fixture path the test chose
		if err != nil {
			t.Fatal(err)
		}
		capture.reset()
		client := *c
		if name == "timeout.pdf" {
			// Docling's own timeout, as a long document meets it.
			client.DocumentTimeout = 10 * time.Millisecond
		}
		start := time.Now()
		ended := convert(t.Context(), &client, pdf, name)
		rec := fakedocling.Recording{SHA256: sha(pdf), Seconds: time.Since(start).Round(10 * time.Millisecond).Seconds()}
		inspect := capture.get("/v1/inspect")
		if inspect == nil {
			t.Fatalf("%s: pdftext never answered (%s)", name, ended)
		}
		rec.Inspect = *inspect
		rec.Result, rec.Lines = capture.get("/v1/result/"), capture.get("/v1/lines")
		var pages struct {
			Pages int `json:"pages"`
		}
		if json.Unmarshal(inspect.Body, &pages) == nil {
			rec.Pages = pages.Pages
		}
		data, err := json.MarshalIndent(rec, "", " ")
		if err != nil {
			t.Fatal(err)
		}
		out := strings.TrimSuffix(path, ".pdf") + fakedocling.Suffix
		if err := os.WriteFile(out, append(data, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s: %s in %.1fs, %d pages", name, map[string]string{"": "converted"}[ended]+ended, rec.Seconds, rec.Pages)
	}
}

// capture keeps the last answer to each route a recording replays.
type capture struct {
	mu      sync.Mutex
	answers map[string]*fakedocling.Answer
}

func (c *capture) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.answers)
}

func (c *capture) get(route string) *fakedocling.Answer {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.answers[route]
}

func (c *capture) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := http.DefaultTransport.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	route := r.URL.Path
	if strings.HasPrefix(route, "/v1/result/") {
		route = "/v1/result/"
	}
	if route != "/v1/inspect" && route != "/v1/lines" && route != "/v1/result/" {
		return resp, nil
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	var compact bytes.Buffer
	if json.Compact(&compact, body) != nil {
		compact.Reset()
		compact.WriteString(`null`)
	}
	c.mu.Lock()
	c.answers[route] = &fakedocling.Answer{Status: resp.StatusCode, Body: compact.Bytes()}
	c.mu.Unlock()
	return resp, nil
}

func sha(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
