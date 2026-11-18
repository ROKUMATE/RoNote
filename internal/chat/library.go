package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/search"
)

// passagesPerSearch is how many chunks a search sends the model: the
// question's own, and each tool call's.
const passagesPerSearch = 8

// toolName is the one tool a model may call.
const toolName = "search_library"

// searchTool searches the library a turn runs in. Its input names neither
// an owner nor a document: those are the conversation's.
var searchTool = Tool{
	Name: toolName,
	Description: "Search the student's library: the documentation pages they imported, and their own notes. " +
		"Returns up to 8 passages, which you can cite. Use it when the passages you have don't answer the question, " +
		"with a query in the words the pages would use.",
	Properties: map[string]any{
		"query": map[string]any{
			"type": "string", "maxLength": search.MaxQuery,
			"description": "What to look for: words, names from code, an error message, or a question.",
		},
		"this_document_only": map[string]any{
			"type":        "boolean",
			"description": "Search only the document this conversation is about. Leave it false to search the whole library.",
		},
	},
	Required: []string{"query"},
}

// libraryTools are the tools a turn offers, until its searches run out.
var libraryTools = []Tool{searchTool, notFoundTool}

// library is one owner's library, as one conversation searches it: the
// conversation's document, or uuid.Nil for the whole library.
type library struct {
	searcher *search.Searcher
	owner    uuid.UUID
	document uuid.UUID
}

// search returns the top passages for query in the conversation's scope,
// or in the owner's whole library when wide.
func (l library) search(ctx context.Context, query string, wide bool) ([]Passage, error) {
	if utf8.RuneCountInString(query) > search.MaxQuery {
		query = string([]rune(query)[:search.MaxQuery])
	}
	r := search.Request{Owner: l.owner, Document: l.document, Query: query, Limit: passagesPerSearch, Mode: search.Hybrid}
	if wide {
		r.Document = uuid.Nil
	}
	res, err := l.searcher.Search(ctx, r)
	if err != nil {
		return nil, err
	}
	passages := make([]Passage, len(res.Hits))
	for i, h := range res.Hits {
		title := strings.Join(append([]string{h.Title}, h.HeadingPath...), " > ")
		if h.Kind == search.KindNote {
			title += " (the student's note)"
		}
		passages[i] = fit(Passage{ChunkID: h.ChunkID, DocumentID: h.DocumentID, Title: title, BlockIDs: h.BlockIDs, Texts: h.BlockTexts})
	}
	return passages, nil
}

// toolInput is what search_library takes. Anything else in a call is an
// error, so a model can't believe it chose an owner or a document.
type toolInput struct {
	Query            string `json:"query"`
	ThisDocumentOnly bool   `json:"this_document_only"`
}

// run answers one tool call, always as the library's owner, and returns the
// passages it sent.
func (l library) run(ctx context.Context, use ToolUse) (ToolResult, []Passage, error) {
	refuse := func(why string) (ToolResult, []Passage, error) {
		return ToolResult{ToolUseID: use.ID, Text: why, IsError: true}, nil, nil
	}
	if use.Name != toolName {
		return refuse(fmt.Sprintf("There is no tool named %q.", use.Name))
	}
	var in toolInput
	dec := json.NewDecoder(bytes.NewReader(use.Input))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return refuse("The input must be {\"query\": string, \"this_document_only\": boolean}: " + err.Error())
	}
	in.Query = strings.TrimSpace(in.Query)
	if in.Query == "" || utf8.RuneCountInString(in.Query) > search.MaxQuery {
		return refuse(fmt.Sprintf("query must be 1 to %d characters.", search.MaxQuery))
	}
	if in.ThisDocumentOnly && l.document == uuid.Nil {
		return refuse("This conversation is about the whole library, not one document: set this_document_only to false.")
	}
	passages, err := l.search(ctx, in.Query, !in.ThisDocumentOnly)
	if err != nil {
		return ToolResult{}, nil, err
	}
	if len(passages) == 0 {
		return ToolResult{ToolUseID: use.ID, Text: "No results found."}, nil, nil
	}
	result := ToolResult{ToolUseID: use.ID, Results: make([]Result, len(passages))}
	for i, p := range passages {
		result.Results[i] = p.result()
	}
	return result, passages, nil
}
