package fakellm

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// event is one server-sent event.
type event struct {
	name string
	data map[string]any
}

// post sends body and returns the status and the whole answer.
func post(t *testing.T, body any) (int, []byte) {
	t.Helper()
	srv := httptest.NewServer(Handler())
	t.Cleanup(srv.Close)
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+"/v1/messages", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	answer, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, answer
}

func streamed(t *testing.T, body any) []event {
	t.Helper()
	status, answer := post(t, body)
	if status != http.StatusOK {
		t.Fatalf("status %d: %s", status, answer)
	}
	var events []event
	var name string
	for line := range strings.Lines(string(answer)) {
		line = strings.TrimSuffix(line, "\n")
		switch {
		case strings.HasPrefix(line, "event: "):
			name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			var data map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &data); err != nil {
				t.Fatal(err)
			}
			if data["type"] != name {
				t.Errorf("event %s carries type %v", name, data["type"])
			}
			events = append(events, event{name, data})
		}
	}
	return events
}

// blockOut is a streamed content block, put back together.
type blockOut struct {
	kind, text, input, signature string
	citations                    []map[string]any
}

func assemble(t *testing.T, events []event) (blocks []blockOut, stop string) {
	t.Helper()
	if events[0].name != "message_start" || events[len(events)-1].name != "message_stop" {
		t.Fatalf("stream runs %s to %s, want message_start to message_stop", events[0].name, events[len(events)-1].name)
	}
	for _, e := range events {
		switch e.name {
		case "content_block_start":
			cb := e.data["content_block"].(map[string]any)
			blocks = append(blocks, blockOut{kind: cb["type"].(string)})
		case "content_block_delta":
			b := &blocks[int(e.data["index"].(float64))]
			d := e.data["delta"].(map[string]any)
			switch d["type"] {
			case "text_delta":
				b.text += d["text"].(string)
			case "citations_delta":
				b.citations = append(b.citations, d["citation"].(map[string]any))
			case "input_json_delta":
				b.input += d["partial_json"].(string)
			case "signature_delta":
				b.signature = d["signature"].(string)
			}
		case "message_delta":
			stop = e.data["delta"].(map[string]any)["stop_reason"].(string)
		}
	}
	return blocks, stop
}

func resultBlock(source, title string, texts ...string) map[string]any {
	content := make([]map[string]any, len(texts))
	for i, text := range texts {
		content[i] = map[string]any{"type": "text", "text": text}
	}
	return map[string]any{"type": "search_result", "source": source, "title": title, "content": content, "citations": map[string]any{"enabled": true}}
}

func text(s string) map[string]any { return map[string]any{"type": "text", "text": s} }

func user(blocks ...map[string]any) map[string]any {
	return map[string]any{"role": "user", "content": blocks}
}

func ask(messages ...map[string]any) map[string]any {
	return map[string]any{
		"model": "claude-haiku-5-5", "max_tokens": 1024, "stream": true,
		"system":        []map[string]any{{"type": "text", "text": "Answer from the sources.", "cache_control": map[string]any{"type": "ephemeral"}}},
		"tools":         []map[string]any{{"name": "search_library", "description": "Search.", "input_schema": map[string]any{"type": "object"}}},
		"thinking":      map[string]any{"type": "adaptive"},
		"output_config": map[string]any{"effort": "low"},
		"messages":      messages,
	}
}

var library = []map[string]any{
	resultBlock("ronote:chunk/1", "Effective Go > Concurrency", "Concurrency is not parallelism.", "Channels let goroutines communicate. Unbuffered ones synchronize."),
	resultBlock("ronote:chunk/2", "Effective Go > Maps", "Maps hold keys and values."),
}

func TestAnswersFromTheResultsItIsSent(t *testing.T) {
	blocks, stop := assemble(t, streamed(t, ask(user(append(library, text("How do goroutines communicate?"))...))))
	if stop != "end_turn" || len(blocks) != 2 {
		t.Fatalf("blocks = %+v, stop %q; want two text blocks and end_turn", blocks, stop)
	}
	if blocks[0].text != "From Effective Go > Concurrency:\n\n" || len(blocks[0].citations) != 0 {
		t.Errorf("first block = %+v, want an uncited lead-in", blocks[0])
	}
	cited := blocks[1]
	if cited.text != "Channels let goroutines communicate." || len(cited.citations) != 1 {
		t.Fatalf("second block = %+v, want the block's first sentence with one citation", cited)
	}
	want := map[string]any{
		"type": "search_result_location", "source": "ronote:chunk/1", "title": "Effective Go > Concurrency",
		"cited_text":          "Channels let goroutines communicate. Unbuffered ones synchronize.",
		"search_result_index": 0.0, "start_block_index": 1.0, "end_block_index": 2.0,
	}
	for k, v := range want {
		if cited.citations[0][k] != v {
			t.Errorf("citation %s = %v, want %v", k, cited.citations[0][k], v)
		}
	}
}

func TestSaysWhenTheLibraryDoesNotCoverIt(t *testing.T) {
	blocks, _ := assemble(t, streamed(t, ask(user(append(library, text("How do I configure ServeMux routes?"))...))))
	if len(blocks) != 1 || len(blocks[0].citations) != 0 {
		t.Fatalf("blocks = %+v, want one uncited text", blocks)
	}
	if want := "Your library doesn't cover this. Import a page about configure and servemux to ask about it."; blocks[0].text != want {
		t.Errorf("text = %q, want %q", blocks[0].text, want)
	}
}

func TestCallsNotInLibraryWhenItCan(t *testing.T) {
	body := ask(user(append(library, text("How do I configure ServeMux routes?"))...))
	body["tools"] = []map[string]any{
		{"name": "search_library", "description": "Search.", "input_schema": map[string]any{"type": "object"}},
		{"name": "not_in_library", "description": "Say so.", "input_schema": map[string]any{"type": "object"}},
	}
	blocks, stop := assemble(t, streamed(t, body))
	if stop != "tool_use" || len(blocks) != 2 || blocks[0].kind != "thinking" || blocks[1].kind != "tool_use" {
		t.Fatalf("blocks = %+v, stop %q; want thinking, then a tool use", blocks, stop)
	}
	var input map[string]string
	if err := json.Unmarshal([]byte(blocks[1].input), &input); err != nil ||
		input["missing"] != "Your library doesn't cover this." || input["suggest"] != "a page about configure and servemux" {
		t.Errorf("tool input = %s (%v), want what is missing and what to import", blocks[1].input, err)
	}
}

func TestSearchesWhenAsked(t *testing.T) {
	question := user(append(library, text("Search for comparable keys"))...)
	blocks, stop := assemble(t, streamed(t, ask(question)))
	if stop != "tool_use" || len(blocks) != 2 || blocks[0].kind != "thinking" || blocks[1].kind != "tool_use" {
		t.Fatalf("blocks = %+v, stop %q; want thinking, then a tool use", blocks, stop)
	}
	var input map[string]any
	if err := json.Unmarshal([]byte(blocks[1].input), &input); err != nil || input["query"] != "comparable keys" || input["this_document_only"] != false {
		t.Errorf("tool input = %s (%v), want the query after \"search for\"", blocks[1].input, err)
	}

	assistant := map[string]any{"role": "assistant", "content": []map[string]any{
		{"type": "thinking", "thinking": "", "signature": blocks[0].signature},
		{"type": "tool_use", "id": "toolu_1", "name": "search_library", "input": input},
	}}
	results := user(map[string]any{"type": "tool_result", "tool_use_id": "toolu_1", "content": []map[string]any{
		resultBlock("ronote:chunk/9", "Notes > Keys", "Keys of maps must be comparable."),
	}})
	blocks, stop = assemble(t, streamed(t, ask(question, assistant, results)))
	if stop != "end_turn" || len(blocks) != 2 {
		t.Fatalf("after the tool round: blocks = %+v, stop %q", blocks, stop)
	}
	// Only the tool's result says "comparable".
	if c := blocks[1].citations[0]; c["search_result_index"] != 2.0 || c["source"] != "ronote:chunk/9" {
		t.Errorf("citation = %v, want the tool's result, the third in the request", c)
	}
}

func TestSlowlyStreamsAWordAtATime(t *testing.T) {
	events := streamed(t, ask(user(resultBlock("ronote:chunk/1", "Go", "Channels connect goroutines."), text("Explain slowly how channels work"))))
	var deltas []string
	for _, e := range events {
		if d, ok := e.data["delta"].(map[string]any); ok && d["type"] == "text_delta" {
			deltas = append(deltas, d["text"].(string))
		}
	}
	if strings.Join(deltas, "|") != "From |Go:\n\n|Channels |connect |goroutines." {
		t.Errorf("deltas = %q, want a word each", deltas)
	}
}

func TestRefusesWhatTheAPIRefuses(t *testing.T) {
	question := text("How do goroutines communicate?")
	with := func(change func(r map[string]any)) map[string]any {
		r := ask(user(append(library, question)...))
		change(r)
		return r
	}
	toolRound := func(assistant []map[string]any, result map[string]any) func(map[string]any) {
		return func(r map[string]any) {
			r["messages"] = []map[string]any{
				user(question),
				{"role": "assistant", "content": assistant},
				user(result),
			}
		}
	}
	toolUse := map[string]any{"type": "tool_use", "id": "toolu_1", "name": "search_library", "input": map[string]any{"query": "x"}}
	thinking := map[string]any{"type": "thinking", "thinking": "", "signature": "fakellm"}
	tests := []struct {
		name    string
		request map[string]any
	}{
		{"not streamed", with(func(r map[string]any) { r["stream"] = false })},
		{"a sampling parameter", with(func(r map[string]any) { r["temperature"] = 0 })},
		{"no model", with(func(r map[string]any) { delete(r, "model") })},
		{"citations on the user's text", with(func(r map[string]any) {
			r["messages"] = []map[string]any{user(map[string]any{"type": "text", "text": "q", "citations": []map[string]any{{"type": "search_result_location"}}})}
		})},
		{"citations on some results only", with(func(r map[string]any) {
			off := resultBlock("ronote:chunk/3", "T", "x")
			off["citations"] = map[string]any{"enabled": false}
			r["messages"] = []map[string]any{user(library[0], off, question)}
		})},
		{"a search result from the assistant", with(func(r map[string]any) {
			r["messages"] = []map[string]any{user(question), {"role": "assistant", "content": []map[string]any{library[0]}}, user(question)}
		})},
		{"the assistant first", with(func(r map[string]any) {
			r["messages"] = []map[string]any{{"role": "assistant", "content": "hi"}}
		})},
		{"a tool result answering nothing", with(toolRound([]map[string]any{thinking, toolUse},
			map[string]any{"type": "tool_result", "tool_use_id": "toolu_2", "content": "none"}))},
		{"search results beside text in a tool result", with(toolRound([]map[string]any{thinking, toolUse},
			map[string]any{"type": "tool_result", "tool_use_id": "toolu_1", "content": []map[string]any{library[0], text("and")}}))},
		{"a tool round without its thinking", with(toolRound([]map[string]any{toolUse},
			map[string]any{"type": "tool_result", "tool_use_id": "toolu_1", "content": "No results found."}))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, answer := post(t, tt.request)
			var body struct {
				Type  string `json:"type"`
				Error struct {
					Type    string `json:"type"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(answer, &body); err != nil {
				t.Fatal(err)
			}
			if status != http.StatusBadRequest || body.Error.Type != "invalid_request_error" {
				t.Errorf("status %d, error %+v; want 400 invalid_request_error", status, body.Error)
			}
		})
	}
}
