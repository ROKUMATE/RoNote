// Package anthropic adapts Anthropic's Messages API to chat.Model
// (docs/adr/0012-chat.md). It is the only package that imports Anthropic's
// SDK.
package anthropic

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	sdk "github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
	"github.com/anthropics/anthropic-sdk-go/v2/shared"

	"github.com/rokumate/ronote/internal/chat"
)

// effort is how hard the model thinks: low, the documented setting for
// chat. Thinking counts toward max_tokens.
const effort = sdk.OutputConfigEffortLow

// Model calls one model through the Messages API, streamed.
type Model struct {
	client sdk.Client
	model  string
}

// New returns a Model that calls model at url with key. RoNote's own config
// names all three: nothing is read from the environment.
func New(url, key, model string) *Model {
	return &Model{
		client: sdk.NewClient(option.WithoutEnvironmentDefaults(), option.WithBaseURL(url), option.WithAPIKey(key)),
		model:  model,
	}
}

// Stream makes one call and streams its reply. The tools and the system
// prompt are cached together: the cache marker on the system prompt covers
// the tools, which come first.
func (m *Model) Stream(ctx context.Context, req chat.Request, on func(chat.Event)) (chat.Reply, error) {
	stream := m.client.Messages.NewStreaming(ctx, m.params(req))
	defer func() { _ = stream.Close() }()
	var msg sdk.Message
	for stream.Next() {
		ev := stream.Current()
		if err := msg.Accumulate(ev); err != nil {
			return chat.Reply{}, fmt.Errorf("%w: %w", chat.ErrUnavailable, err)
		}
		switch e := ev.AsAny().(type) {
		case sdk.ContentBlockStartEvent:
			if e.ContentBlock.Type == "text" {
				on(chat.TextStart{})
			}
		case sdk.ContentBlockDeltaEvent:
			switch d := e.Delta.AsAny().(type) {
			case sdk.TextDelta:
				on(chat.TextDelta{Text: d.Text})
			case sdk.CitationsDelta:
				on(cited(d.Citation))
			}
		}
	}
	if err := stream.Err(); err != nil {
		return chat.Reply{}, classify(ctx, err)
	}
	return reply(&msg), nil
}

func (m *Model) params(req chat.Request) sdk.MessageNewParams {
	p := sdk.MessageNewParams{
		Model:        m.model,
		MaxTokens:    int64(req.MaxTokens),
		System:       []sdk.TextBlockParam{{Text: req.System, CacheControl: sdk.NewCacheControlEphemeralParam()}},
		OutputConfig: sdk.OutputConfigParam{Effort: effort},
	}
	for _, t := range req.Tools {
		p.Tools = append(p.Tools, sdk.ToolUnionParam{OfTool: &sdk.ToolParam{
			Name:        t.Name,
			Description: sdk.String(t.Description),
			InputSchema: sdk.ToolInputSchemaParam{Properties: t.Properties, Required: t.Required},
		}})
	}
	if req.ToolsOff {
		p.ToolChoice = sdk.ToolChoiceUnionParam{OfNone: &sdk.ToolChoiceNoneParam{}}
	}
	for _, msg := range req.Messages {
		blocks := make([]sdk.ContentBlockParamUnion, 0, len(msg.Blocks))
		for _, b := range msg.Blocks {
			blocks = append(blocks, blockParam(b))
		}
		if msg.Role == chat.Assistant {
			p.Messages = append(p.Messages, sdk.NewAssistantMessage(blocks...))
		} else {
			p.Messages = append(p.Messages, sdk.NewUserMessage(blocks...))
		}
	}
	return p
}

func blockParam(b chat.Block) sdk.ContentBlockParamUnion {
	switch b := b.(type) {
	case chat.Text:
		return sdk.NewTextBlock(b.Text)
	case chat.Result:
		return sdk.ContentBlockParamUnion{OfSearchResult: searchResult(b)}
	case chat.ToolUse:
		return sdk.ContentBlockParamUnion{OfToolUse: &sdk.ToolUseBlockParam{ID: b.ID, Name: b.Name, Input: b.Input}}
	case chat.ToolResult:
		content := make([]sdk.ToolResultBlockParamContentUnion, 0, len(b.Results))
		for _, r := range b.Results {
			content = append(content, sdk.ToolResultBlockParamContentUnion{OfSearchResult: searchResult(r)})
		}
		if len(b.Results) == 0 {
			content = append(content, sdk.ToolResultBlockParamContentUnion{OfText: &sdk.TextBlockParam{Text: b.Text}})
		}
		result := &sdk.ToolResultBlockParam{ToolUseID: b.ToolUseID, Content: content}
		if b.IsError {
			result.IsError = sdk.Bool(true)
		}
		return sdk.ContentBlockParamUnion{OfToolResult: result}
	case chat.Thinking:
		return sdk.ContentBlockParamUnion{OfThinking: &sdk.ThinkingBlockParam{Thinking: b.Thinking, Signature: b.Signature}}
	case chat.RedactedThinking:
		return sdk.ContentBlockParamUnion{OfRedactedThinking: &sdk.RedactedThinkingBlockParam{Data: b.Data}}
	}
	panic(fmt.Sprintf("anthropic: no content block for %T", b))
}

// searchResult is r with citations on: they are all-or-none in a request.
func searchResult(r chat.Result) *sdk.SearchResultBlockParam {
	content := make([]sdk.TextBlockParam, len(r.Texts))
	for i, text := range r.Texts {
		content[i] = sdk.TextBlockParam{Text: text}
	}
	return &sdk.SearchResultBlockParam{
		Source: r.Source, Title: r.Title, Content: content, Citations: sdk.CitationsConfigParam{Enabled: sdk.Bool(true)},
	}
}

func cited(c sdk.CitationsDeltaCitationUnion) chat.Cited {
	if c.Type != "search_result_location" {
		return chat.Cited{ResultIndex: -1}
	}
	return chat.Cited{
		ResultIndex: int(c.SearchResultIndex), Source: c.Source,
		Start: int(c.StartBlockIndex), End: int(c.EndBlockIndex),
	}
}

func reply(msg *sdk.Message) chat.Reply {
	r := chat.Reply{
		Stop: string(msg.StopReason),
		Usage: chat.Usage{
			InputTokens: int(msg.Usage.InputTokens), OutputTokens: int(msg.Usage.OutputTokens),
			CacheCreationInputTokens: int(msg.Usage.CacheCreationInputTokens), CacheReadInputTokens: int(msg.Usage.CacheReadInputTokens),
		},
	}
	// The accumulated fields, not AsAny: they hold every delta even when a
	// block never saw its stop event.
	for _, b := range msg.Content {
		switch b.Type {
		case "text":
			r.Content = append(r.Content, chat.Text{Text: b.Text})
		case "tool_use":
			use := chat.ToolUse{ID: b.ID, Name: b.Name, Input: b.Input}
			r.Content = append(r.Content, use)
			r.ToolUses = append(r.ToolUses, use)
		case "thinking":
			r.Content = append(r.Content, chat.Thinking{Thinking: b.Thinking, Signature: b.Signature})
		case "redacted_thinking":
			r.Content = append(r.Content, chat.RedactedThinking{Data: b.Data})
		}
	}
	return r
}

// classify wraps err as chat.ErrUnavailable when asking again may work: a
// rate limit, an overload, a server error or no answer. Any other refusal
// is chat.ErrRejected; a context that ended stays as it is. An error sent
// mid-stream comes with the stream's 200, so its type decides, not its
// status.
func classify(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return err
	}
	var apiErr *sdk.Error
	if !errors.As(err, &apiErr) {
		return fmt.Errorf("%w: %w", chat.ErrUnavailable, err)
	}
	switch apiErr.Type() {
	case shared.ErrorTypeRateLimitError, shared.ErrorTypeOverloadedError, shared.ErrorTypeAPIError, shared.ErrorTypeTimeoutError:
		return fmt.Errorf("%w: %w", chat.ErrUnavailable, err)
	case "":
		if apiErr.StatusCode == http.StatusTooManyRequests || apiErr.StatusCode >= 500 {
			return fmt.Errorf("%w: %w", chat.ErrUnavailable, err)
		}
	}
	return fmt.Errorf("%w: %w", chat.ErrRejected, err)
}
