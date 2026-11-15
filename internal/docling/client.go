package docling

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

// Error codes a PDF fails with when docling or pdftext refuse it for good
// (docs/adr/0013-pdf-import.md); they are the API's.
const (
	CodePasswordProtected = "pdf_password_protected"
	CodeTooManyPages      = "pdf_too_many_pages"
	CodeScanned           = "scanned_pdf_unsupported"
	CodeFailed            = "pdf_conversion_failed"
)

// MaxPages is the most pages a PDF may have.
const MaxPages = 300

// ServeVersion is the docling-serve release RoNote is pinned to; the dev
// stack runs its image, and extractions record it.
const ServeVersion = "1.38.2"

// ErrUnavailable wraps errors retrying may get past: timeouts, 5xx and
// network errors, from docling or pdftext.
var ErrUnavailable = errors.New("docling: unavailable")

// RefusedError is a PDF docling or pdftext can't convert, whatever the
// number of tries; Code says why.
type RefusedError struct {
	Code   string
	Reason string
}

func (e *RefusedError) Error() string { return "docling: " + e.Code + ": " + e.Reason }

// Service is an HTTP API RoNote calls with a key.
type Service struct {
	URL string
	Key string
}

// Client converts PDFs with docling-serve's async API and reads their code
// with pdftext.
type Client struct {
	Docling Service
	PDFText Service
	HTTP    *http.Client
	// PollEvery spaces docling status polls.
	PollEvery time.Duration
}

// Inspection is what pdftext found opening a PDF.
type Inspection struct {
	Pages int `json:"pages"`
	Chars int `json:"chars"`
}

// Inspect opens pdf with pdftext. A password-protected or unreadable PDF,
// one over MaxPages, or one without a text layer is a *RefusedError.
func (c *Client) Inspect(ctx context.Context, pdf []byte) (Inspection, error) {
	var found struct {
		Inspection
		Error string `json:"error"`
	}
	status, err := c.post(ctx, c.PDFText, "/v1/inspect", map[string][]byte{"file": pdf}, &found)
	if err != nil {
		return Inspection{}, err
	}
	switch {
	case status == http.StatusUnprocessableEntity && found.Error == "password":
		return Inspection{}, &RefusedError{Code: CodePasswordProtected, Reason: "the PDF needs a password"}
	case status == http.StatusUnprocessableEntity:
		return Inspection{}, &RefusedError{Code: CodeFailed, Reason: "pdfium can't open the PDF"}
	case status != http.StatusOK:
		return Inspection{}, fmt.Errorf("%w: pdftext inspect answered %d", ErrUnavailable, status)
	case found.Pages > MaxPages:
		return Inspection{}, &RefusedError{Code: CodeTooManyPages, Reason: fmt.Sprintf("%d pages", found.Pages)}
	case found.Pages > 0 && found.Chars == 0:
		return Inspection{}, &RefusedError{Code: CodeScanned, Reason: "no text layer"}
	}
	return found.Inspection, nil
}

// Code reads the lines of each code box from pdf's text layer.
func (c *Client) Code(ctx context.Context, pdf []byte, boxes []Box) ([][]string, error) {
	if len(boxes) == 0 {
		return nil, nil
	}
	data, err := json.Marshal(boxes)
	if err != nil {
		return nil, err
	}
	var found struct {
		Blocks [][]string `json:"blocks"`
	}
	status, err := c.post(ctx, c.PDFText, "/v1/code", map[string][]byte{"file": pdf, "boxes": data}, &found)
	switch {
	case err != nil:
		return nil, err
	case status == http.StatusUnprocessableEntity:
		return nil, &RefusedError{Code: CodeFailed, Reason: "pdftext can't read the code boxes"}
	case status != http.StatusOK:
		return nil, fmt.Errorf("%w: pdftext code answered %d", ErrUnavailable, status)
	case len(found.Blocks) != len(boxes):
		return nil, fmt.Errorf("%w: pdftext answered %d blocks for %d boxes", ErrUnavailable, len(found.Blocks), len(boxes))
	}
	return found.Blocks, nil
}

// task is docling's async task, as submit and poll answer it.
type task struct {
	ID     string `json:"task_id"`
	Status string `json:"task_status"`
}

// result is docling's conversion result.
type result struct {
	Status   string `json:"status"`
	Document struct {
		JSON *Document `json:"json_content"`
	} `json:"document"`
	Errors []struct {
		Message  string `json:"error_message"`
		Category string `json:"category"`
	} `json:"errors"`
}

// Options asks docling for JSON only, from the text layer: no OCR, no
// images, no enrichment models, and headings at their inferred levels.
var options = map[string][]string{
	"to_formats":               {"json"},
	"do_ocr":                   {"false"},
	"include_images":           {"false"},
	"image_export_mode":        {"placeholder"},
	"do_pdf_heading_hierarchy": {"true"},
	"abort_on_error":           {"true"},
}

// Convert submits pdf to docling, polls until it is done, and returns its
// document. Docling's timeouts are ErrUnavailable; a PDF it refuses is a
// *RefusedError.
func (c *Client) Convert(ctx context.Context, pdf []byte, filename string) (*Document, error) {
	var t task
	status, err := c.postForm(ctx, c.Docling, "/v1/convert/file/async", filename, pdf, &t)
	switch {
	case err != nil:
		return nil, err
	case status != http.StatusOK || t.ID == "":
		return nil, fmt.Errorf("%w: docling submit answered %d", ErrUnavailable, status)
	}
	id := t.ID
	for t.Status != "success" && t.Status != "failure" {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("%w: %w", ErrUnavailable, ctx.Err())
		case <-time.After(c.PollEvery):
		}
		if status, err = c.get(ctx, c.Docling, "/v1/status/poll/"+id, &t); err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("%w: docling poll answered %d", ErrUnavailable, status)
		}
	}
	var r result
	if status, err = c.get(ctx, c.Docling, "/v1/result/"+id, &r); err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("%w: docling result answered %d", ErrUnavailable, status)
	}
	return r.document()
}

// document sorts docling's result: timeouts are worth another try, its page
// limit and anything else it fails on are not. The API refuses files over
// the size limit before docling sees them.
func (r *result) document() (*Document, error) {
	for _, e := range r.Errors {
		if e.Category == "timeout" {
			return nil, fmt.Errorf("%w: docling: %s", ErrUnavailable, e.Message)
		}
	}
	for _, e := range r.Errors {
		if e.Category == "policy" && strings.Contains(e.Message, "max_num_pages") {
			return nil, &RefusedError{Code: CodeTooManyPages, Reason: e.Message}
		}
	}
	if r.Status != "success" || r.Document.JSON == nil {
		reason := r.Status
		if len(r.Errors) > 0 {
			reason += ": " + r.Errors[0].Message
		}
		return nil, &RefusedError{Code: CodeFailed, Reason: reason}
	}
	return r.Document.JSON, nil
}

// post sends parts as a multipart form to svc and decodes a JSON answer
// into out.
func (c *Client) post(ctx context.Context, svc Service, path string, parts map[string][]byte, out any) (int, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for _, name := range []string{"file", "boxes"} {
		data, ok := parts[name]
		if !ok {
			continue
		}
		var part io.Writer
		var err error
		if name == "file" {
			part, err = w.CreateFormFile(name, "document.pdf")
		} else {
			part, err = w.CreateFormField(name)
		}
		if err != nil {
			return 0, err
		}
		if _, err := part.Write(data); err != nil {
			return 0, err
		}
	}
	if err := w.Close(); err != nil {
		return 0, err
	}
	return c.do(ctx, svc, http.MethodPost, path, w.FormDataContentType(), &body, out)
}

// postForm submits pdf to docling with options.
func (c *Client) postForm(ctx context.Context, svc Service, path, filename string, pdf []byte, out any) (int, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("files", filename)
	if err != nil {
		return 0, err
	}
	if _, err := part.Write(pdf); err != nil {
		return 0, err
	}
	for _, name := range []string{"to_formats", "do_ocr", "include_images", "image_export_mode", "do_pdf_heading_hierarchy", "abort_on_error"} {
		for _, v := range options[name] {
			if err := w.WriteField(name, v); err != nil {
				return 0, err
			}
		}
	}
	if err := w.Close(); err != nil {
		return 0, err
	}
	return c.do(ctx, svc, http.MethodPost, path, w.FormDataContentType(), &body, out)
}

func (c *Client) get(ctx context.Context, svc Service, path string, out any) (int, error) {
	return c.do(ctx, svc, http.MethodGet, path, "", nil, out)
}

// do sends a request with svc's key. Network errors and 5xx answers are
// ErrUnavailable; a 2xx or 422 answer is decoded into out.
func (c *Client) do(ctx context.Context, svc Service, method, path, contentType string, body io.Reader, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimSuffix(svc.URL, "/")+path, body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("X-Api-Key", svc.Key)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 500 {
		return resp.StatusCode, fmt.Errorf("%w: %s %s answered %d", ErrUnavailable, method, path, resp.StatusCode)
	}
	if resp.StatusCode/100 == 2 || resp.StatusCode == http.StatusUnprocessableEntity {
		// A docling result is as large as its document: a 300-page book's
		// JSON runs to tens of megabytes.
		if err := json.NewDecoder(io.LimitReader(resp.Body, 256<<20)).Decode(out); err != nil {
			return resp.StatusCode, fmt.Errorf("%w: %s %s: %w", ErrUnavailable, method, path, err)
		}
	}
	return resp.StatusCode, nil
}
