package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

func TestRequestID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		incoming string
		wantKept bool
	}{
		{name: "generated when missing", incoming: ""},
		{name: "propagated when valid", incoming: "req-42.a_b", wantKept: true},
		{name: "propagated at 64 chars", incoming: strings.Repeat("a", 64), wantKept: true},
		{name: "replaced when it has a space", incoming: "req 42"},
		{name: "replaced when longer than 64 chars", incoming: strings.Repeat("a", 65)},
		{name: "replaced when it has a newline", incoming: "req\n42"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := newRequest(t, http.MethodGet, "/nope")
			if tt.incoming != "" {
				req.Header.Set(requestIDHeader, tt.incoming)
			}
			rec := record(New(discardLogger), req)

			got := rec.Header().Get(requestIDHeader)
			switch {
			case tt.wantKept && got != tt.incoming:
				t.Errorf("X-Request-ID = %q, want incoming %q", got, tt.incoming)
			case !tt.wantKept && (got == tt.incoming || len(got) != 26 || !validRequestID.MatchString(got)):
				t.Errorf("X-Request-ID = %q, want a new 26-char ID", got)
			}
			if body := decodeError(t, rec); body.RequestID != got {
				t.Errorf("error request_id = %q, want %q", body.RequestID, got)
			}
		})
	}
}

func TestRecovery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantStatus int
		wantBody   string
	}{
		{
			name:       "panic before writing",
			handler:    func(http.ResponseWriter, *http.Request) { panic("boom") },
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "panic after writing",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusAccepted)
				panic("boom")
			},
			wantStatus: http.StatusAccepted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			rec := record(withMiddleware(logger, tt.handler), newRequest(t, http.MethodGet, "/"))

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			id := rec.Header().Get(requestIDHeader)
			if tt.wantStatus == http.StatusInternalServerError {
				body := decodeError(t, rec)
				if body.Code != "internal_error" || body.RequestID != id {
					t.Errorf("error = %+v, want code internal_error and request_id %q", body, id)
				}
			} else if rec.Body.Len() != 0 {
				t.Errorf("body = %q, want nothing written after the panic", rec.Body)
			}

			lines := logLines(t, &logs)
			if len(lines) != 2 {
				t.Fatalf("got %d log lines, want panic and access log:\n%s", len(lines), logs.String())
			}
			if lines[0]["msg"] != "panic" || lines[0]["request_id"] != id || lines[0]["stack"] == "" {
				t.Errorf("panic log = %v, want msg panic, request_id %q and a stack", lines[0], id)
			}
			if got := lines[1]["status"]; got != float64(tt.wantStatus) {
				t.Errorf("access log status = %v, want %d", got, tt.wantStatus)
			}
		})
	}
}

func TestRecoveryReraisesErrAbortHandler(t *testing.T) {
	t.Parallel()

	h := withMiddleware(discardLogger, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))
	defer func() {
		if err, _ := recover().(error); !errors.Is(err, http.ErrAbortHandler) {
			t.Errorf("recovered %v, want http.ErrAbortHandler", err)
		}
	}()
	record(h, newRequest(t, http.MethodGet, "/"))
}

func TestAccessLog(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	req := newRequest(t, http.MethodGet, "/healthz")
	req.Header.Set(requestIDHeader, "req-1")
	record(New(slog.New(slog.NewJSONHandler(&logs, nil))), req)

	lines := logLines(t, &logs)
	if len(lines) != 1 {
		t.Fatalf("got %d log lines, want 1:\n%s", len(lines), logs.String())
	}
	line := lines[0]
	want := map[string]any{
		"msg":        "request",
		"method":     "GET",
		"path":       "/healthz",
		"status":     float64(http.StatusOK),
		"request_id": "req-1",
	}
	for k, v := range want {
		if line[k] != v {
			t.Errorf("log %s = %v, want %v", k, line[k], v)
		}
	}
	if _, ok := line["duration"].(float64); !ok {
		t.Errorf("log duration = %v, want a number", line["duration"])
	}
}

func logLines(t *testing.T, logs *bytes.Buffer) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for raw := range bytes.Lines(logs.Bytes()) {
		var line map[string]any
		if err := json.Unmarshal(raw, &line); err != nil {
			t.Fatalf("log line %q is not JSON: %v", raw, err)
		}
		lines = append(lines, line)
	}
	return lines
}
