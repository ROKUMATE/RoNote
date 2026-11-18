package chat

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rokumate/ronote/internal/store"
)

func TestParseNotFound(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		want    store.NotFound
		wantWhy string
	}{
		{name: "both said", input: `{"missing":" Nothing on CORS. ","suggest":"the FastAPI page on CORS"}`, want: store.NotFound{Missing: "Nothing on CORS.", Suggest: "the FastAPI page on CORS"}},
		{name: "no suggestion", input: `{"missing":"Nothing.","suggest":" "}`, wantWhy: "both"},
		{name: "a URL", input: `{"missing":"Nothing.","suggest":"https://example.com/cors"}`, wantWhy: "URL"},
		{name: "www", input: `{"missing":"See WWW.example.com","suggest":"a page"}`, wantWhy: "URL"},
		{name: "too long", input: `{"missing":"` + strings.Repeat("x", maxMissing+1) + `","suggest":"a page"}`, wantWhy: "at most"},
		{name: "another field", input: `{"missing":"a","suggest":"b","owner_id":"x"}`, wantWhy: "input must be"},
		{name: "not an object", input: `"nothing"`, wantWhy: "input must be"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, why := parseNotFound(ToolUse{Input: json.RawMessage(tt.input)})
			if got != tt.want || (tt.wantWhy == "") != (why == "") || !strings.Contains(why, tt.wantWhy) {
				t.Errorf("parseNotFound = %+v, %q; want %+v, %q", got, why, tt.want, tt.wantWhy)
			}
		})
	}
}

func TestExchangesSendNotFoundAnswers(t *testing.T) {
	t.Parallel()
	msgs := []store.Message{
		{Role: store.RoleUser, Status: store.MessageDone, Content: store.MessageContent{Segments: []store.Segment{{Text: "How do I enable CORS?"}}}},
		{Role: store.RoleAssistant, Status: store.MessageNotFound, Content: store.MessageContent{NotFound: &store.NotFound{Missing: "Nothing on CORS.", Suggest: "the FastAPI page on CORS"}}},
	}
	got := exchanges(msgs)
	if len(got) != 1 || got[0].question != "How do I enable CORS?" || !strings.Contains(got[0].answer, "Nothing on CORS.") || !strings.Contains(got[0].answer, "the FastAPI page on CORS") {
		t.Errorf("exchanges = %+v, want the not_found answer told as what it lacks and what to import", got)
	}
}
