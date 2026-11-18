// Package fakellm is a deterministic stand-in for Anthropic's Messages API
// (docs/adr/0012-chat.md), for dev and the end-to-end tests (cmd/fakellm):
// they run the real adapter and need no model. It serves the subset RoNote
// uses, streamed only: text, search results with citations, tool use and
// thinking blocks. It refuses what the API would refuse within that subset,
// such as citations on a user's text or search results with citations both
// on and off.
//
// It answers from the search results it is sent: it cites the first block,
// in request order, that holds one of the question's longer words, and
// otherwise says the library doesn't cover the question. Two phrases steer
// it: "search for X" first calls the search_library tool with X, and
// "slowly" streams the answer a word at a time, slowly enough to stop.
package fakellm

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

const (
	// toolName is the tool "search for" calls, and notFoundName the one an
	// unmatched question calls.
	toolName     = "search_library"
	notFoundName = "not_in_library"
	// slowDelay is the pause between words when a question asks "slowly".
	slowDelay = 150 * time.Millisecond
	// maxRequest bounds what the handler reads.
	maxRequest = 8 << 20
	// sentenceChars bounds the sentence an answer cites.
	sentenceChars = 200
)

// Handler serves POST /v1/messages and GET /healthz. It answers for
// whatever model is asked.
func Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok\n")) })
	mux.HandleFunc("POST /v1/messages", messages)
	return mux
}

type request struct {
	Model        string          `json:"model"`
	MaxTokens    int             `json:"max_tokens"`
	Stream       bool            `json:"stream"`
	System       json.RawMessage `json:"system"`
	Tools        []tool          `json:"tools"`
	Thinking     *typed          `json:"thinking"`
	OutputConfig json.RawMessage `json:"output_config"`
	Messages     []message       `json:"messages"`
}

type typed struct {
	Type string `json:"type"`
}

type tool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"input_schema"`
	CacheControl json.RawMessage `json:"cache_control"`
}

type message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// block is any content block of the subset: fields a type doesn't use stay
// empty.
type block struct {
	Type         string          `json:"type"`
	Text         string          `json:"text"`
	Citations    json.RawMessage `json:"citations"`
	Source       string          `json:"source"`
	Title        string          `json:"title"`
	Content      json.RawMessage `json:"content"`
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Input        json.RawMessage `json:"input"`
	ToolUseID    string          `json:"tool_use_id"`
	IsError      bool            `json:"is_error"`
	Thinking     string          `json:"thinking"`
	Signature    string          `json:"signature"`
	Data         string          `json:"data"`
	CacheControl json.RawMessage `json:"cache_control"`
}

// result is a search result as sent: its blocks' texts.
type result struct {
	source, title string
	blocks        []string
}

// turn is what a request asks: the latest question, every search result
// in request order, and whether a tool round followed the question.
type turn struct {
	question    string
	results     []result
	searched    bool
	canSearch   bool
	canNotFound bool
	thinking    bool
}

func messages(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequest))
	if err != nil {
		invalid(w, err.Error())
		return
	}
	var req request
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		invalid(w, err.Error())
		return
	}
	t, err := read(&req)
	if err != nil {
		invalid(w, err.Error())
		return
	}
	// About 4 characters a token.
	stream(w, r, req.Model, len(body)/4, answer(t))
}

// read checks req against the API's rules for the subset and returns its
// turn.
func read(req *request) (turn, error) {
	switch {
	case req.Model == "":
		return turn{}, errors.New("model: Field required")
	case req.MaxTokens < 1:
		return turn{}, errors.New("max_tokens: must be at least 1")
	case !req.Stream:
		return turn{}, errors.New("fakellm only streams: set stream to true")
	case len(req.Messages) == 0 || req.Messages[0].Role != "user":
		return turn{}, errors.New("messages: the first message must be from the user")
	}
	t := turn{thinking: req.Thinking == nil || req.Thinking.Type != "disabled"}
	t.canSearch = slices.ContainsFunc(req.Tools, func(tl tool) bool { return tl.Name == toolName })
	t.canNotFound = slices.ContainsFunc(req.Tools, func(tl tool) bool { return tl.Name == notFoundName })
	var citations []bool
	var lastToolUses []string
	questionAt := -1
	for i, m := range req.Messages {
		blocks, err := blocksOf(m.Content)
		if err != nil {
			return turn{}, fmt.Errorf("messages.%d.content: %w", i, err)
		}
		switch m.Role {
		case "user":
			var text []string
			for j, b := range blocks {
				where := fmt.Sprintf("messages.%d.content.%d", i, j)
				switch b.Type {
				case "text":
					if hasCitations(b.Citations) {
						return turn{}, fmt.Errorf("%s: citations are not allowed on text in a user message", where)
					}
					text = append(text, b.Text)
				case "search_result":
					res, enabled, err := searchResult(b)
					if err != nil {
						return turn{}, fmt.Errorf("%s: %w", where, err)
					}
					t.results = append(t.results, res)
					citations = append(citations, enabled)
				case "tool_result":
					if !slices.Contains(lastToolUses, b.ToolUseID) {
						return turn{}, fmt.Errorf("%s: tool_use_id %q answers no tool_use of the previous message", where, b.ToolUseID)
					}
					results, enabled, err := toolResult(b)
					if err != nil {
						return turn{}, fmt.Errorf("%s: %w", where, err)
					}
					t.results = append(t.results, results...)
					citations = append(citations, enabled...)
					if questionAt >= 0 {
						t.searched = true
					}
				default:
					return turn{}, fmt.Errorf("%s: fakellm doesn't take %q blocks from the user", where, b.Type)
				}
			}
			if len(text) > 0 {
				t.question, questionAt, t.searched = strings.Join(text, "\n"), i, false
			}
			lastToolUses = nil
		case "assistant":
			lastToolUses = nil
			thought := false
			for j, b := range blocks {
				switch b.Type {
				case "text":
				case "thinking", "redacted_thinking":
					thought = true
				case "tool_use":
					lastToolUses = append(lastToolUses, b.ID)
				default:
					return turn{}, fmt.Errorf("messages.%d.content.%d: %q blocks can't be in an assistant message", i, j, b.Type)
				}
			}
			if t.thinking && len(lastToolUses) > 0 && i == len(req.Messages)-2 && !thought {
				return turn{}, fmt.Errorf("messages.%d: with thinking on, pass the thinking blocks of a tool use back unchanged", i)
			}
		default:
			return turn{}, fmt.Errorf("messages.%d.role: %q is not user or assistant", i, m.Role)
		}
	}
	if slices.Contains(citations, true) && slices.Contains(citations, false) {
		return turn{}, errors.New("citations must be enabled on all search results or on none")
	}
	if questionAt < 0 {
		return turn{}, errors.New("messages: no user text to answer")
	}
	return t, nil
}

// blocksOf reads content: a string, or a list of blocks.
func blocksOf(content json.RawMessage) ([]block, error) {
	var text string
	if json.Unmarshal(content, &text) == nil {
		return []block{{Type: "text", Text: text}}, nil
	}
	var blocks []block
	if err := json.Unmarshal(content, &blocks); err != nil {
		return nil, err
	}
	return blocks, nil
}

func hasCitations(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s != "" && s != "null" && s != "[]"
}

func searchResult(b block) (result, bool, error) {
	if b.Source == "" {
		return result{}, false, errors.New("source: Field required")
	}
	var setting struct {
		Enabled bool `json:"enabled"`
	}
	if hasCitations(b.Citations) {
		if err := json.Unmarshal(b.Citations, &setting); err != nil {
			return result{}, false, fmt.Errorf("citations: %w", err)
		}
	}
	var items []block
	if err := json.Unmarshal(b.Content, &items); err != nil || len(items) == 0 {
		return result{}, false, errors.New("content: a search result needs text blocks")
	}
	res := result{source: b.Source, title: b.Title}
	for _, item := range items {
		if item.Type != "text" {
			return result{}, false, fmt.Errorf("content: a search result holds text only, not %q", item.Type)
		}
		res.blocks = append(res.blocks, item.Text)
	}
	return res, setting.Enabled, nil
}

// toolResult reads a tool result's search results: it holds them alone, or
// none.
func toolResult(b block) ([]result, []bool, error) {
	blocks, err := blocksOf(b.Content)
	if err != nil {
		return nil, nil, err
	}
	var results []result
	var enabled []bool
	for _, item := range blocks {
		if item.Type != "search_result" {
			continue
		}
		res, on, err := searchResult(item)
		if err != nil {
			return nil, nil, err
		}
		results, enabled = append(results, res), append(enabled, on)
	}
	if len(results) > 0 && len(results) != len(blocks) {
		return nil, nil, errors.New("content: a tool result with search results can hold nothing else")
	}
	return results, enabled, nil
}

// reply is what to stream back.
type reply struct {
	blocks []out
	stop   string
	slow   bool
}

// out is one content block of a reply: text, with a citation or none; a
// tool use with its input; or an empty thinking block.
type out struct {
	kind     string
	name     string
	text     string
	citation map[string]any
	input    string
}

var searchFor = regexp.MustCompile(`(?i)\bsearch for\s+(.+?)[?.!]*$`)

func answer(t turn) reply {
	slow := strings.Contains(strings.ToLower(t.question), "slowly")
	if m := searchFor.FindStringSubmatch(t.question); m != nil && !t.searched && t.canSearch {
		var blocks []out
		if t.thinking {
			blocks = append(blocks, out{kind: "thinking"})
		}
		input, _ := json.Marshal(map[string]any{"query": m[1], "this_document_only": false})
		return reply{blocks: append(blocks, out{kind: "tool_use", name: toolName, input: string(input)}), stop: "tool_use"}
	}
	words := keywords(t.question)
	for i, res := range t.results {
		for b, text := range res.blocks {
			lower := strings.ToLower(text)
			if !slices.ContainsFunc(words, func(w string) bool { return strings.Contains(lower, w) }) {
				continue
			}
			cited := sentence(text)
			if slow {
				cited = text
			}
			return reply{slow: slow, stop: "end_turn", blocks: []out{
				{kind: "text", text: "From " + res.title + ":\n\n"},
				{kind: "text", text: cited, citation: map[string]any{
					"type": "search_result_location", "cited_text": text, "source": res.source, "title": res.title,
					"search_result_index": i, "start_block_index": b, "end_block_index": b + 1,
				}},
			}}
		}
	}
	suggest := "a page on the topic"
	if len(words) > 0 {
		suggest = "a page about " + strings.Join(words[:min(len(words), 2)], " and ")
	}
	if t.canNotFound {
		var blocks []out
		if t.thinking {
			blocks = append(blocks, out{kind: "thinking"})
		}
		input, _ := json.Marshal(map[string]any{"missing": "Your library doesn't cover this.", "suggest": suggest})
		return reply{slow: slow, stop: "tool_use", blocks: append(blocks, out{kind: "tool_use", name: notFoundName, input: string(input)})}
	}
	return reply{slow: slow, stop: "end_turn", blocks: []out{{kind: "text", text: "Your library doesn't cover this. Import " + suggest + " to ask about it."}}}
}

var (
	word = regexp.MustCompile(`[a-z0-9_]+`)
	// common words that say nothing about a page, and the steering words.
	common = []string{"about", "could", "explain", "search", "should", "slowly", "their", "there", "these", "which", "would"}
)

// keywords are the question's words of 5 letters or more, lowercased, in
// order, without common ones.
func keywords(question string) []string {
	var out []string
	for _, w := range word.FindAllString(strings.ToLower(question), -1) {
		if len(w) >= 5 && !slices.Contains(common, w) && !slices.Contains(out, w) {
			out = append(out, w)
		}
	}
	return out
}

// sentence is text's first sentence, at most sentenceChars characters.
func sentence(text string) string {
	text = strings.TrimSpace(text)
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = text[:i]
	}
	if i := strings.Index(text, ". "); i >= 0 {
		text = text[:i+1]
	}
	if utf8.RuneCountInString(text) > sentenceChars {
		text = string([]rune(text)[:sentenceChars]) + "…"
	}
	return text
}

var ids atomic.Int64

// stream writes rep as the API's server-sent events.
func stream(w http.ResponseWriter, r *http.Request, model string, inputTokens int, rep reply) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	n := ids.Add(1)
	send := func(event string, data map[string]any) bool {
		data["type"] = event
		payload, _ := json.Marshal(data)
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	delta := func(index int, d map[string]any) bool {
		return send("content_block_delta", map[string]any{"index": index, "delta": d})
	}
	send("message_start", map[string]any{"message": map[string]any{
		"id": fmt.Sprintf("msg_fake_%d", n), "type": "message", "role": "assistant", "model": model,
		"content": []any{}, "stop_reason": nil, "stop_sequence": nil,
		"usage": map[string]any{"input_tokens": inputTokens, "output_tokens": 1, "cache_creation_input_tokens": 0, "cache_read_input_tokens": 0},
	}})
	send("ping", map[string]any{})
	outputTokens := 1
	for i, b := range rep.blocks {
		switch b.kind {
		case "thinking":
			send("content_block_start", map[string]any{"index": i, "content_block": map[string]any{"type": "thinking", "thinking": "", "signature": ""}})
			delta(i, map[string]any{"type": "thinking_delta", "thinking": ""})
			delta(i, map[string]any{"type": "signature_delta", "signature": "fakellm"})
		case "tool_use":
			send("content_block_start", map[string]any{"index": i, "content_block": map[string]any{
				"type": "tool_use", "id": fmt.Sprintf("toolu_fake_%d", n), "name": b.name, "input": map[string]any{},
			}})
			delta(i, map[string]any{"type": "input_json_delta", "partial_json": b.input})
			outputTokens += len(b.input) / 4
		case "text":
			send("content_block_start", map[string]any{"index": i, "content_block": map[string]any{"type": "text", "text": ""}})
			for _, piece := range pieces(b.text, rep.slow) {
				if rep.slow {
					select {
					case <-r.Context().Done():
						return
					case <-time.After(slowDelay):
					}
				}
				if !delta(i, map[string]any{"type": "text_delta", "text": piece}) {
					return
				}
			}
			if b.citation != nil {
				delta(i, map[string]any{"type": "citations_delta", "citation": b.citation})
			}
			outputTokens += len(b.text) / 4
		}
		send("content_block_stop", map[string]any{"index": i})
	}
	send("message_delta", map[string]any{
		"delta": map[string]any{"stop_reason": rep.stop, "stop_sequence": nil},
		"usage": map[string]any{"output_tokens": outputTokens},
	})
	send("message_stop", map[string]any{})
}

// pieces splits text into the deltas it streams as: a word each when slow,
// else a few words each.
func pieces(text string, slow bool) []string {
	words := strings.SplitAfter(text, " ")
	size := 4
	if slow {
		size = 1
	}
	var out []string
	for chunk := range slices.Chunk(words, size) {
		out = append(out, strings.Join(chunk, ""))
	}
	return out
}

// invalid answers as the API does a request it refuses.
func invalid(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type": "error", "error": map[string]string{"type": "invalid_request_error", "message": message},
	})
}
