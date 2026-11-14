// Package chat answers a student's questions from their own library, with
// answers that cite its blocks (docs/adr/0012-chat.md). It talks to a
// language model through the port in this file; internal/chat/anthropic
// adapts Anthropic's Messages API to it.
package chat

import (
	"context"
	"encoding/json"
	"errors"
)

// Model is a language model, one streamed call at a time. It is the port
// between the chat service and a model's API, and the seam its tests drive.
type Model interface {
	// Stream sends req, calls on with each event as it arrives, and
	// returns the whole reply once it ends.
	Stream(ctx context.Context, req Request, on func(Event)) (Reply, error)
}

// ModelFunc is a function as a Model, as tests script one.
type ModelFunc func(ctx context.Context, req Request, on func(Event)) (Reply, error)

// Stream calls f.
func (f ModelFunc) Stream(ctx context.Context, req Request, on func(Event)) (Reply, error) {
	return f(ctx, req, on)
}

// Errors a Model returns, wrapped.
var (
	// ErrUnavailable means the model's API didn't answer, was overloaded or
	// limited the rate: asking again later may work.
	ErrUnavailable = errors.New("the model is unavailable")
	// ErrRejected means the API refused the request itself: asking again
	// won't help.
	ErrRejected = errors.New("the model rejected the request")
)

// Request is one call to a model.
type Request struct {
	System string
	Tools  []Tool
	// ToolsOff lets the model call none of its tools in this call, so it
	// answers with what it has.
	ToolsOff  bool
	Messages  []Message
	MaxTokens int
}

// Tool is a tool the model may call, with its input's JSON Schema
// properties.
type Tool struct {
	Name        string
	Description string
	Properties  map[string]any
	Required    []string
}

// Role is who a message is from.
type Role string

// Roles.
const (
	User      Role = "user"
	Assistant Role = "assistant"
)

// Message is one message to a model.
type Message struct {
	Role   Role
	Blocks []Block
}

// Block is part of a message: Text, Result, ToolUse, ToolResult, Thinking
// or RedactedThinking.
type Block interface{ block() }

// Text is plain text.
type Text struct{ Text string }

// Result is a search result, its content a text per block, with citations
// on. Only users send them, directly or in a ToolResult.
type Result struct {
	Source string
	Title  string
	Texts  []string
}

// ToolUse is the model calling a tool.
type ToolUse struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// ToolResult answers a ToolUse with search results, or with Text when
// there are none.
type ToolResult struct {
	ToolUseID string
	Results   []Result
	Text      string
	IsError   bool
}

// Thinking is the model's reasoning, sent back unchanged within a turn's
// tool rounds.
type Thinking struct {
	Thinking  string
	Signature string
}

// RedactedThinking is reasoning the API sends encrypted, sent back the same
// way.
type RedactedThinking struct{ Data string }

func (Text) block()             {}
func (Result) block()           {}
func (ToolUse) block()          {}
func (ToolResult) block()       {}
func (Thinking) block()         {}
func (RedactedThinking) block() {}

// Event is something a reply says as it streams: TextStart, TextDelta or
// Cited.
type Event interface{ event() }

// TextStart starts a block of text; the citations that follow are its.
type TextStart struct{}

// TextDelta is more of the current block of text.
type TextDelta struct{ Text string }

// Cited is a citation of the current block of text: blocks Start to End,
// End excluded, of the search result at ResultIndex among all the results
// in the request, in order. ResultIndex is -1 for a citation of anything
// else.
type Cited struct {
	ResultIndex int
	Source      string
	Start, End  int
}

func (TextStart) event() {}
func (TextDelta) event() {}
func (Cited) event()     {}

// Stop reasons a reply can end with.
const (
	StopEndTurn   = "end_turn"
	StopToolUse   = "tool_use"
	StopMaxTokens = "max_tokens"
	StopRefusal   = "refusal"
)

// Reply is a whole reply.
type Reply struct {
	// Content is the reply's blocks, as the next call of a tool round
	// sends them back.
	Content []Block
	// ToolUses are the tools it calls, in order.
	ToolUses []ToolUse
	Stop     string
	Usage    Usage
}

// Usage is the tokens a call read and wrote.
type Usage struct {
	InputTokens              int
	OutputTokens             int
	CacheCreationInputTokens int
	CacheReadInputTokens     int
}

// Add returns u with v's tokens added.
func (u Usage) Add(v Usage) Usage {
	return Usage{
		InputTokens:              u.InputTokens + v.InputTokens,
		OutputTokens:             u.OutputTokens + v.OutputTokens,
		CacheCreationInputTokens: u.CacheCreationInputTokens + v.CacheCreationInputTokens,
		CacheReadInputTokens:     u.CacheReadInputTokens + v.CacheReadInputTokens,
	}
}
