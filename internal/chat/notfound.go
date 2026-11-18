package chat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/rokumate/ronote/internal/store"
)

// notFoundName is the tool a model calls to say the library doesn't answer.
const notFoundName = "not_in_library"

// Bounds on what the model says it lacks, in characters.
const (
	maxMissing = 300
	maxSuggest = 200
)

// notFoundTool ends a turn: the model calls it instead of answering when
// the library doesn't hold the answer (docs/adr/0012-chat.md).
var notFoundTool = Tool{
	Name: notFoundName,
	Description: "Call this instead of answering when the student's library doesn't answer the question, " +
		"after searching it if the passages you were given didn't. The turn ends and the student is told it isn't in their library. " +
		"Don't call it when the passages answer part of the question: answer that part.",
	Properties: map[string]any{
		"missing": map[string]any{
			"type": "string", "maxLength": maxMissing,
			"description": "One sentence on what the library lacks, in plain words. Don't say what an outside page would say.",
		},
		"suggest": map[string]any{
			"type": "string", "maxLength": maxSuggest,
			"description": "The kind of page the student could import to find the answer, such as the official documentation of the library or tool the question is about. No URLs.",
		},
	},
	Required: []string{"missing", "suggest"},
}

// notFoundInput is what not_in_library takes.
type notFoundInput struct {
	Missing string `json:"missing"`
	Suggest string `json:"suggest"`
}

// parseNotFound reads a not_in_library call. A call that can't be shown to
// a student returns why, which goes back to the model as a tool error.
func parseNotFound(use ToolUse) (store.NotFound, string) {
	var in notFoundInput
	dec := json.NewDecoder(bytes.NewReader(use.Input))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return store.NotFound{}, "The input must be {\"missing\": string, \"suggest\": string}: " + err.Error()
	}
	nf := store.NotFound{Missing: strings.TrimSpace(in.Missing), Suggest: strings.TrimSpace(in.Suggest)}
	switch {
	case nf.Missing == "" || nf.Suggest == "":
		return store.NotFound{}, "missing and suggest must both say something."
	case utf8.RuneCountInString(nf.Missing) > maxMissing:
		return store.NotFound{}, fmt.Sprintf("missing must be at most %d characters.", maxMissing)
	case utf8.RuneCountInString(nf.Suggest) > maxSuggest:
		return store.NotFound{}, fmt.Sprintf("suggest must be at most %d characters.", maxSuggest)
	case hasURL(nf.Missing) || hasURL(nf.Suggest):
		return store.NotFound{}, "Name the kind of page, not a URL."
	}
	return nf, ""
}

func hasURL(s string) bool {
	s = strings.ToLower(s)
	return strings.Contains(s, "http://") || strings.Contains(s, "https://") || strings.Contains(s, "www.")
}

// notFoundText is what a later turn is told the answer was.
func notFoundText(n store.NotFound) string {
	return strings.TrimSpace(n.Missing + " To find it, the student could import " + n.Suggest)
}
