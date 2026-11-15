// Package fakedocling replays recorded answers of docling-serve and pdftext
// (docs/adr/0013-pdf-import.md), so dev and the end-to-end tests convert
// PDFs without the docling image. make pdf-record records the answers for
// each fixture in testdata/pdf; a PDF without a recording gets 503. Dev and
// test only, served by cmd/fakedocling.
package fakedocling

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/uuid"
)

// Recording is what docling and pdftext answered for one PDF.
type Recording struct {
	// SHA256 is the PDF's, hex: what an upload is matched by.
	SHA256 string `json:"sha256"`
	// Seconds is how long the real conversion took, and Pages how many
	// pages it had, for the report.
	Seconds float64 `json:"seconds,omitempty"`
	Pages   int     `json:"pages,omitempty"`
	Inspect Answer  `json:"inspect"`
	// Result is docling's result, and Lines pdftext's lines of its boxes;
	// absent when the import stops before them.
	Result *Answer `json:"result,omitempty"`
	Lines  *Answer `json:"lines,omitempty"`
}

// Answer is one recorded HTTP answer.
type Answer struct {
	Status int             `json:"status"`
	Body   json.RawMessage `json:"body"`
}

// Suffix ends every recording's filename, after the PDF's name.
const Suffix = ".docling.json"

// Load reads every recording in dir, by the SHA-256 of its PDF.
func Load(dir string) (map[string]Recording, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*"+Suffix))
	if err != nil {
		return nil, err
	}
	recs := map[string]Recording{}
	for _, p := range paths {
		data, err := os.ReadFile(p) //nolint:gosec // a recording in the directory the command was given
		if err != nil {
			return nil, err
		}
		var r Recording
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		recs[r.SHA256] = r
	}
	return recs, nil
}

// Keys are the API keys the real services would check.
type Keys struct {
	Docling string
	PDFText string
}

// Handler serves docling's async API and pdftext's routes from recs.
func Handler(keys Keys, recs map[string]Recording) http.Handler {
	s := &server{recs: recs, tasks: map[string]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		reply(w, http.StatusOK, json.RawMessage(`{"status":"ok"}`))
	})
	mux.Handle("POST /v1/inspect", keyed(keys.PDFText, s.answer("file", func(r Recording) *Answer { return &r.Inspect })))
	mux.Handle("POST /v1/lines", keyed(keys.PDFText, s.answer("file", func(r Recording) *Answer { return r.Lines })))
	mux.Handle("POST /v1/convert/file/async", keyed(keys.Docling, http.HandlerFunc(s.submit)))
	mux.Handle("GET /v1/status/poll/{id}", keyed(keys.Docling, http.HandlerFunc(s.poll)))
	mux.Handle("GET /v1/result/{id}", keyed(keys.Docling, s.result()))
	return mux
}

type server struct {
	recs  map[string]Recording
	mu    sync.Mutex
	tasks map[string]string // task id -> PDF SHA-256
}

func keyed(key string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != key {
			reply(w, http.StatusUnauthorized, json.RawMessage(`{"detail":"Unauthorized"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// recording finds the recording of the PDF uploaded as field.
func (s *server) recording(r *http.Request, field string) (Recording, bool) {
	file, _, err := r.FormFile(field)
	if err != nil {
		return Recording{}, false
	}
	defer func() { _ = file.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return Recording{}, false
	}
	rec, ok := s.recs[hex.EncodeToString(h.Sum(nil))]
	return rec, ok
}

func (s *server) answer(field string, pick func(Recording) *Answer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec, ok := s.recording(r, field)
		if !ok {
			unknown(w)
			return
		}
		a := pick(rec)
		if a == nil {
			reply(w, http.StatusServiceUnavailable, json.RawMessage(`{"error":"not recorded"}`))
			return
		}
		reply(w, a.Status, a.Body)
	})
}

func (s *server) submit(w http.ResponseWriter, r *http.Request) {
	rec, ok := s.recording(r, "files")
	if !ok {
		unknown(w)
		return
	}
	id := uuid.NewString()
	s.mu.Lock()
	s.tasks[id] = rec.SHA256
	s.mu.Unlock()
	reply(w, http.StatusOK, json.RawMessage(`{"task_id":"`+id+`","task_type":"convert","task_status":"pending","task_position":1}`))
}

// task is the recording a task id was given for.
func (s *server) task(r *http.Request) (Recording, bool) {
	s.mu.Lock()
	sha, ok := s.tasks[r.PathValue("id")]
	s.mu.Unlock()
	if !ok {
		return Recording{}, false
	}
	rec, ok := s.recs[sha]
	return rec, ok
}

func (s *server) poll(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.task(r); !ok {
		reply(w, http.StatusNotFound, json.RawMessage(`{"detail":"Task not found."}`))
		return
	}
	reply(w, http.StatusOK, json.RawMessage(`{"task_id":"`+r.PathValue("id")+`","task_type":"convert","task_status":"success"}`))
}

func (s *server) result() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec, ok := s.task(r)
		if !ok || rec.Result == nil {
			reply(w, http.StatusNotFound, json.RawMessage(`{"detail":"Task result not found."}`))
			return
		}
		reply(w, rec.Result.Status, rec.Result.Body)
	})
}

func unknown(w http.ResponseWriter) {
	reply(w, http.StatusServiceUnavailable, json.RawMessage(`{"error":"no recording of this PDF; run make pdf-record"}`))
}

func reply(w http.ResponseWriter, status int, body json.RawMessage) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body) //nolint:gosec // recorded JSON, served as JSON
}
