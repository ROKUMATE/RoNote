package anthropic

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rokumate/ronote/internal/chat"
	"github.com/rokumate/ronote/internal/fakellm"
)

var (
	update = flag.Bool("update", false, "rewrite testdata/requests from what the adapter sends")
	record = flag.Bool("record", false, "record testdata/streams through Anthropic's API (needs ANTHROPIC_API_KEY)")
)

const (
	model     = "claude-haiku-5-5"
	anthropic = "https://api.anthropic.com"
)

var (
	channels = chat.Result{
		Source: "ronote:chunk/01930000-0000-7000-8000-000000000001", Title: "Effective Go > Concurrency > Channels",
		Texts: []string{"Do not communicate by sharing memory; instead, share memory by communicating.", "Channels let goroutines communicate."},
	}
	unbuffered = chat.Result{
		Source: "ronote:chunk/01930000-0000-7000-8000-000000000002", Title: "Effective Go > Concurrency > Channels",
		Texts: []string{"Unbuffered channels combine communication with synchronization."},
	}
	semaphore = chat.Result{
		Source: "ronote:chunk/01930000-0000-7000-8000-000000000003", Title: "Effective Go > Concurrency > Channels",
		Texts: []string{"A buffered channel can be used like a semaphore, for instance to limit throughput."},
	}
	searchTool = chat.Tool{
		Name: "search_library", Description: "Search the student's library.",
		Properties: map[string]any{
			"query":              map[string]any{"type": "string"},
			"this_document_only": map[string]any{"type": "boolean"},
		},
		Required: []string{"query"},
	}
)

func ask(question string, results ...chat.Result) chat.Request {
	blocks := make([]chat.Block, 0, len(results)+1)
	for _, r := range results {
		blocks = append(blocks, r)
	}
	return chat.Request{
		System:    "Answer the student's question from their library, and cite it.",
		Tools:     []chat.Tool{searchTool},
		Messages:  []chat.Message{{Role: chat.User, Blocks: append(blocks, chat.Text{Text: question})}},
		MaxTokens: 1024,
	}
}

// replay serves stream's events for one request and checks that request
// against its golden. With -record it asks Anthropic instead and records
// the answer.
func replay(t *testing.T, stream string) *Model {
	t.Helper()
	streamFile := filepath.Join("testdata", "streams", stream+".sse")
	key := ""
	if *record {
		if key = os.Getenv("ANTHROPIC_API_KEY"); key == "" {
			t.Skip("-record needs ANTHROPIC_API_KEY")
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		golden(t, stream, body)
		if *record {
			proxy(t, w, r, body, streamFile)
			return
		}
		events, err := os.ReadFile(streamFile) //nolint:gosec // a fixture name the test chose
		if err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(events)
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, key, model)
}

// golden compares the request body with testdata/requests/<name>.json, or
// rewrites it with -update or -record.
func golden(t *testing.T, name string, body []byte) {
	t.Helper()
	file := filepath.Join("testdata", "requests", name+".json")
	var got any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Errorf("request is not JSON: %v", err)
		return
	}
	if *update || *record {
		var pretty bytes.Buffer
		enc := json.NewEncoder(&pretty)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(got); err != nil {
			t.Error(err)
		}
		if err := os.MkdirAll(filepath.Dir(file), 0o750); err != nil {
			t.Error(err)
		}
		if err := os.WriteFile(file, pretty.Bytes(), 0o644); err != nil { //nolint:gosec // test data
			t.Error(err)
		}
		return
	}
	data, err := os.ReadFile(file) //nolint:gosec // a golden name the test chose
	if err != nil {
		t.Errorf("%v (run with -update)", err)
		return
	}
	var want any
	if err := json.Unmarshal(data, &want); err != nil {
		t.Error(err)
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("request differs from %s (run with -update and review the diff):\n%s", file, body)
	}
}

// proxy sends the request on to Anthropic and records the answer.
func proxy(t *testing.T, w http.ResponseWriter, r *http.Request, body []byte, file string) {
	t.Helper()
	req, err := http.NewRequestWithContext(r.Context(), r.Method, anthropic+r.URL.Path, bytes.NewReader(body)) //nolint:gosec // only ever Anthropic's API, with -record
	if err != nil {
		t.Error(err)
		return
	}
	req.Header = r.Header.Clone()
	// The adapter's own Accept-Encoding would make the transport hand back
	// the gzipped body, and record that; without it, Go asks for gzip
	// itself and records the events as they decode.
	req.Header.Del("Accept-Encoding")
	res, err := http.DefaultClient.Do(req) //nolint:gosec // only ever Anthropic's API, with -record
	if err != nil {
		t.Error(err)
		return
	}
	defer func() { _ = res.Body.Close() }()
	events, err := io.ReadAll(res.Body)
	if err != nil {
		t.Error(err)
		return
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("Anthropic answered %d: %s", res.StatusCode, events)
	} else if err := os.WriteFile(file, events, 0o644); err != nil { //nolint:gosec // test data
		t.Error(err)
	}
	w.Header().Set("Content-Type", res.Header.Get("Content-Type"))
	w.WriteHeader(res.StatusCode)
	_, _ = w.Write(events)
}

// heard is what a reply streamed.
type heard struct {
	texts []string
	cited [][]chat.Cited
}

func (h *heard) on(ev chat.Event) {
	switch e := ev.(type) {
	case chat.TextStart:
		h.texts = append(h.texts, "")
		h.cited = append(h.cited, nil)
	case chat.TextDelta:
		h.texts[len(h.texts)-1] += e.Text
	case chat.Cited:
		h.cited[len(h.cited)-1] = append(h.cited[len(h.cited)-1], e)
	}
}

// checkCitations fails unless every citation points into the results sent.
func checkCitations(t *testing.T, h heard, sent []chat.Result) {
	t.Helper()
	for _, cs := range h.cited {
		for _, c := range cs {
			if c.ResultIndex < 0 || c.ResultIndex >= len(sent) || sent[c.ResultIndex].Source != c.Source ||
				c.Start < 0 || c.Start >= c.End || c.End > len(sent[c.ResultIndex].Texts) {
				t.Errorf("citation %+v points at nothing sent", c)
			}
		}
	}
}

func TestAnswerWithCitations(t *testing.T) {
	m := replay(t, "answer")
	var h heard
	reply, err := m.Stream(t.Context(), ask("How do goroutines communicate?", channels, unbuffered), h.on)
	if err != nil {
		t.Fatal(err)
	}
	if reply.Stop != chat.StopEndTurn || len(reply.ToolUses) != 0 {
		t.Errorf("reply = %+v, want an answer", reply)
	}
	if strings.Join(h.texts, "") == "" || len(h.cited) == 0 {
		t.Fatalf("streamed %q with citations %v, want cited text", h.texts, h.cited)
	}
	checkCitations(t, h, []chat.Result{channels, unbuffered})
	if reply.Usage.InputTokens == 0 || reply.Usage.OutputTokens == 0 {
		t.Errorf("usage = %+v, want tokens both ways", reply.Usage)
	}
	if *record {
		return
	}
	// What Haiku 5.5 answered, recorded 2026-11-15: text blocks, a citation
	// on two of them, and the system prompt and tools read from the cache.
	want := []string{
		"Goroutines communicate through channels. ",
		"Channels let goroutines communicate.",
		" The guidance behind this design favors message passing over shared memory. Go's approach steers developers away from shared-memory access and toward exchanging values through channels ",
		"Do not communicate by sharing memory; instead, share memory by communicating.",
		"\n\nUnbuffered channels also do more than transfer data. A send on one is paired with a receive, so the two goroutines also coordinate their timing.",
	}
	if !reflect.DeepEqual(h.texts, want) {
		t.Errorf("texts = %q, want %q", h.texts, want)
	}
	wantCited := [][]chat.Cited{nil, {{ResultIndex: 0, Source: channels.Source, Start: 1, End: 2}}, nil, {{ResultIndex: 0, Source: channels.Source, Start: 0, End: 1}}, nil}
	if !reflect.DeepEqual(h.cited, wantCited) {
		t.Errorf("cited = %+v, want %+v", h.cited, wantCited)
	}
	if want := (chat.Usage{InputTokens: 412, OutputTokens: 635, CacheReadInputTokens: 1118}); reply.Usage != want {
		t.Errorf("usage = %+v, want %+v", reply.Usage, want)
	}
}

// TestAnswerTheResultsDontHold asks what the results sent don't answer. A
// real model says so, and may cite what the library does hold: whatever it
// cites must point at what was sent.
func TestAnswerTheResultsDontHold(t *testing.T) {
	m := replay(t, "no_answer")
	var h heard
	// No tools: offered them, Haiku 5.5 searches the library before it says
	// the results don't hold the answer; with them turned off, it once
	// thought and ended its turn without a word (docs/open-questions.md).
	req := ask("How do I configure ServeMux routes?", channels, unbuffered)
	req.Tools = nil
	reply, err := m.Stream(t.Context(), req, h.on)
	if err != nil {
		t.Fatal(err)
	}
	if reply.Stop != chat.StopEndTurn || len(reply.ToolUses) != 0 || strings.Join(h.texts, "") == "" {
		t.Errorf("reply = %+v, texts %q; want a plain answer", reply, h.texts)
	}
	checkCitations(t, h, []chat.Result{channels, unbuffered})
}

func TestToolRound(t *testing.T) {
	first := ask("Search my library for using a buffered channel as a semaphore", channels)
	var h heard
	reply, err := replay(t, "tool_use").Stream(t.Context(), first, h.on)
	if err != nil {
		t.Fatal(err)
	}
	if reply.Stop != chat.StopToolUse || len(reply.ToolUses) != 1 || reply.ToolUses[0].Name != "search_library" {
		t.Fatalf("reply = %+v, want one call of search_library", reply)
	}
	var input struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(reply.ToolUses[0].Input, &input); err != nil || input.Query == "" {
		t.Errorf("tool input %s (%v), want a query", reply.ToolUses[0].Input, err)
	}
	// Haiku 5.5 thinks first and returns the block with its signature and,
	// display being omitted by default, no text: it goes back as it came.
	if thinking, ok := reply.Content[0].(chat.Thinking); !ok || thinking.Signature == "" {
		t.Errorf("content = %+v, want the thinking block first, with its signature", reply.Content)
	}

	// The thinking and the tool use go back unchanged, then the results.
	second := first
	second.Messages = append(second.Messages,
		chat.Message{Role: chat.Assistant, Blocks: reply.Content},
		chat.Message{Role: chat.User, Blocks: []chat.Block{chat.ToolResult{ToolUseID: reply.ToolUses[0].ID, Results: []chat.Result{semaphore}}}},
	)
	h = heard{}
	answer, err := replay(t, "tool_answer").Stream(t.Context(), second, h.on)
	if err != nil {
		t.Fatal(err)
	}
	if answer.Stop != chat.StopEndTurn || len(h.cited) == 0 {
		t.Fatalf("answer = %+v, cited %v; want a cited answer", answer, h.cited)
	}
	// The tool's result is the second in the request.
	checkCitations(t, h, []chat.Result{channels, semaphore})
}

func TestErrors(t *testing.T) {
	if *record {
		t.Skip("errors can't be recorded on demand")
	}
	stream := func(status int, body string) error {
		t.Helper()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			// Asking again would only slow the test down.
			w.Header().Set("x-should-retry", "false")
			if status == http.StatusOK {
				w.Header().Set("Content-Type", "text/event-stream")
			} else {
				w.Header().Set("Content-Type", "application/json")
			}
			w.WriteHeader(status)
			_, _ = io.WriteString(w, body)
		}))
		defer srv.Close()
		_, err := New(srv.URL, "", model).Stream(t.Context(), ask("q", channels), func(chat.Event) {})
		return err
	}
	overloaded, err := os.ReadFile("testdata/streams/overloaded.sse")
	if err != nil {
		t.Fatal(err)
	}
	apiError := func(kind string) string {
		return `{"type":"error","error":{"type":"` + kind + `","message":"m"}}`
	}
	tests := []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"overloaded mid-stream", http.StatusOK, string(overloaded), chat.ErrUnavailable},
		{"overloaded", 529, apiError("overloaded_error"), chat.ErrUnavailable},
		{"rate limited", http.StatusTooManyRequests, apiError("rate_limit_error"), chat.ErrUnavailable},
		{"server error", http.StatusInternalServerError, apiError("api_error"), chat.ErrUnavailable},
		{"invalid request", http.StatusBadRequest, apiError("invalid_request_error"), chat.ErrRejected},
		{"bad key", http.StatusUnauthorized, apiError("authentication_error"), chat.ErrRejected},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := stream(tt.status, tt.body); !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

// TestAgainstTheFakeModel runs a tool round against cmd/fakellm's handler,
// which refuses what the API would: the two must agree on the subset.
func TestAgainstTheFakeModel(t *testing.T) {
	srv := httptest.NewServer(fakellm.Handler())
	t.Cleanup(srv.Close)
	m := New(srv.URL, "", model)
	first := ask("Search for semaphore", channels)
	reply, err := m.Stream(t.Context(), first, func(chat.Event) {})
	if err != nil {
		t.Fatal(err)
	}
	if reply.Stop != chat.StopToolUse || len(reply.ToolUses) != 1 {
		t.Fatalf("reply = %+v, want a tool use", reply)
	}
	second := first
	second.Messages = append(second.Messages,
		chat.Message{Role: chat.Assistant, Blocks: reply.Content},
		chat.Message{Role: chat.User, Blocks: []chat.Block{chat.ToolResult{ToolUseID: reply.ToolUses[0].ID, Results: []chat.Result{semaphore}}}},
	)
	var h heard
	if _, err := m.Stream(t.Context(), second, h.on); err != nil {
		t.Fatal(err)
	}
	want := [][]chat.Cited{nil, {{ResultIndex: 1, Source: semaphore.Source, Start: 0, End: 1}}}
	if !reflect.DeepEqual(h.cited, want) {
		t.Errorf("cited = %+v, want the tool's result", h.cited)
	}

	// With no results, the tool answers in text; and with tools off, the
	// model answers.
	third := first
	third.ToolsOff = true
	third.Messages = append(third.Messages,
		chat.Message{Role: chat.Assistant, Blocks: reply.Content},
		chat.Message{Role: chat.User, Blocks: []chat.Block{chat.ToolResult{ToolUseID: reply.ToolUses[0].ID, Text: "No results found."}}},
	)
	if last, err := m.Stream(t.Context(), third, func(chat.Event) {}); err != nil || last.Stop != chat.StopEndTurn {
		t.Errorf("with tools off: %+v, %v; want an answer", last, err)
	}
}
