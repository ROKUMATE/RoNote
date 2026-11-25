package docling

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// stand answers docling's and pdftext's routes with handlers, checking every
// request carries the key.
func stand(t *testing.T, routes map[string]http.HandlerFunc) *Client {
	t.Helper()
	const key = "k"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != key {
			t.Errorf("%s %s without the key", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		h, ok := routes[r.Method+" "+r.URL.Path]
		if !ok {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	svc := Service{URL: srv.URL, Key: key}
	return &Client{Docling: svc, PDFText: svc, HTTP: srv.Client(), PollEvery: time.Millisecond}
}

func answer(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

func refusedCode(err error) string {
	var r *RefusedError
	if errors.As(err, &r) {
		return r.Code
	}
	return ""
}

func TestConvert(t *testing.T) {
	t.Parallel()
	const doc = `{"body": {"children": [{"$ref": "#/texts/0"}]}, "texts": [{"self_ref": "#/texts/0", "label": "text", "text": "Hi", "prov": [{"page_no": 1}]}], "pages": {"1": {"page_no": 1}}}`
	tests := []struct {
		name     string
		result   http.HandlerFunc
		wantCode string
		wantErr  error
	}{
		{name: "success", result: answer(200, `{"status": "success", "document": {"json_content": `+doc+`}, "errors": []}`)},
		{
			name:     "docling's own timeout is final: the same PDF on the same CPUs times out again",
			result:   answer(200, `{"status": "partial_success", "document": {"json_content": `+doc+`}, "errors": [{"error_message": "document timeout exceeded", "category": "timeout"}]}`),
			wantCode: CodeTimeout,
		},
		{
			name:     "over the page limit",
			result:   answer(200, `{"status": "failure", "document": {"json_content": null}, "errors": [{"error_message": "Document has 301 pages, exceeding the max_num_pages limit of 300.", "category": "policy"}]}`),
			wantCode: CodeTooManyPages,
		},
		{
			name:     "a PDF docling can't load",
			result:   answer(200, `{"status": "failure", "document": {"json_content": null}, "errors": [{"error_message": "docling-parse could not load document", "category": "backend_failure"}]}`),
			wantCode: CodeFailed,
		},
		{name: "a result that never comes", result: answer(503, `{}`), wantErr: ErrUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var polls atomic.Int32
			c := stand(t, map[string]http.HandlerFunc{
				"POST /v1/convert/file/async": func(w http.ResponseWriter, r *http.Request) {
					if err := r.ParseMultipartForm(1 << 20); err != nil {
						t.Error(err)
					}
					if got := r.MultipartForm.Value["to_formats"]; !reflect.DeepEqual(got, []string{"json"}) {
						t.Errorf("to_formats = %v", got)
					}
					if got := r.MultipartForm.Value["do_ocr"]; !reflect.DeepEqual(got, []string{"false"}) {
						t.Errorf("do_ocr = %v", got)
					}
					if f := r.MultipartForm.File["files"]; len(f) != 1 || f[0].Filename != "Lecture 3.pdf" {
						t.Errorf("files = %v", f)
					}
					answer(200, `{"task_id": "t1", "task_status": "pending"}`)(w, r)
				},
				"GET /v1/status/poll/t1": func(w http.ResponseWriter, r *http.Request) {
					status := "started"
					if polls.Add(1) >= 3 {
						status = "success"
					}
					answer(200, `{"task_id": "t1", "task_status": "`+status+`"}`)(w, r)
				},
				"GET /v1/result/t1": tt.result,
			})

			got, err := c.Convert(t.Context(), []byte("%PDF-1.7"), "Lecture 3.pdf")

			switch {
			case tt.wantErr != nil && !errors.Is(err, tt.wantErr):
				t.Fatalf("Convert() = %v, want %v", err, tt.wantErr)
			case tt.wantCode != "" && refusedCode(err) != tt.wantCode:
				t.Fatalf("Convert() = %v, want refused with %s", err, tt.wantCode)
			case tt.wantErr == nil && tt.wantCode == "" && (err != nil || len(got.Texts) != 1):
				t.Fatalf("Convert() = %+v, %v", got, err)
			}
			if polls.Load() != 3 {
				t.Errorf("polled %d times, want until success, 3", polls.Load())
			}
		})
	}
}

func TestConvertStopsWithItsContext(t *testing.T) {
	t.Parallel()
	c := stand(t, map[string]http.HandlerFunc{
		"POST /v1/convert/file/async": answer(200, `{"task_id": "t1", "task_status": "pending"}`),
		"GET /v1/status/poll/t1":      answer(200, `{"task_id": "t1", "task_status": "started"}`),
	})
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if _, err := c.Convert(ctx, []byte("%PDF-"), "a.pdf"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Convert() past its deadline = %v, want ErrUnavailable", err)
	}
}

func TestInspect(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		answer   http.HandlerFunc
		want     Inspection
		wantCode string
		wantErr  error
	}{
		{name: "a PDF with text", answer: answer(200, `{"pages": 3, "chars": 900}`), want: Inspection{Pages: 3, Chars: 900}},
		{name: "a password", answer: answer(422, `{"error": "password"}`), wantCode: CodePasswordProtected},
		{name: "unreadable", answer: answer(422, `{"error": "unreadable"}`), wantCode: CodeFailed},
		// A refusal from what pdftext found comes with it: the import shows
		// the page count.
		{name: "one page over the cap", answer: answer(200, `{"pages": 101, "chars": 2300}`), want: Inspection{Pages: 101, Chars: 2300}, wantCode: CodeTooManyPages},
		{name: "at the cap", answer: answer(200, `{"pages": 100, "chars": 2300}`), want: Inspection{Pages: 100, Chars: 2300}},
		{name: "a scan", answer: answer(200, `{"pages": 2, "chars": 0}`), want: Inspection{Pages: 2}, wantCode: CodeScanned},
		{name: "pdftext down", answer: answer(502, ``), wantErr: ErrUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := stand(t, map[string]http.HandlerFunc{"POST /v1/inspect": tt.answer})
			got, err := c.Inspect(t.Context(), []byte("%PDF-"))
			switch {
			case tt.wantErr != nil && !errors.Is(err, tt.wantErr):
				t.Errorf("Inspect() = %v, want %v", err, tt.wantErr)
			case tt.wantCode != "" && refusedCode(err) != tt.wantCode:
				t.Errorf("Inspect() = %v, want refused with %s", err, tt.wantCode)
			case tt.wantErr == nil && tt.wantCode == "" && err != nil:
				t.Errorf("Inspect() = %v, want %+v", err, tt.want)
			case got != tt.want:
				t.Errorf("Inspect() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestLines(t *testing.T) {
	t.Parallel()
	boxes := []Box{{Ref: "#/texts/0", Page: 1, L: 1, T: 2, R: 3, B: 0}, {Ref: "#/texts/1", Page: 2}}
	c := stand(t, map[string]http.HandlerFunc{"POST /v1/lines": func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
		}
		var got []map[string]float64
		if err := json.Unmarshal([]byte(r.MultipartForm.Value["boxes"][0]), &got); err != nil {
			t.Error(err)
		}
		if want := (map[string]float64{"page": 1, "l": 1, "t": 2, "r": 3, "b": 0}); !reflect.DeepEqual(got[0], want) {
			t.Errorf("box sent = %v, want %v", got[0], want)
		}
		answer(200, `{"blocks": [[{"text": "Filename: a.py", "mono": false}, {"text": "def f():", "mono": true}], []]}`)(w, r)
	}})
	got, err := c.Lines(t.Context(), []byte("%PDF-"), boxes)
	want := [][]Line{{{Text: "Filename: a.py"}, {Text: "def f():", Mono: true}}, {}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("Lines() = %+v, %v", got, err)
	}

	short := stand(t, map[string]http.HandlerFunc{"POST /v1/lines": answer(200, `{"blocks": [[]]}`)})
	if _, err := short.Lines(t.Context(), []byte("%PDF-"), boxes); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Lines() with too few blocks = %v, want ErrUnavailable", err)
	}
	if got, err := short.Lines(t.Context(), []byte("%PDF-"), nil); got != nil || err != nil {
		t.Errorf("Lines() of no boxes = %v, %v; want no request", got, err)
	}
}

func TestServeVersionMatchesCompose(t *testing.T) {
	t.Parallel()
	compose, err := os.ReadFile(filepath.Join("..", "..", "deploy", "compose.dev.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(compose), "docling-serve-cpu:v"+ServeVersion+"@sha256:"); n != 2 {
		t.Errorf("deploy/compose.dev.yml runs docling-serve %s %d times, want docling and pdftext", ServeVersion, n)
	}
}
