package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/store/sqlcdb"
)

// RunningFor is longer than any answer runs (docs/adr/0012-chat.md): an
// answer still streaming after it failed with its process.
const RunningFor = 5 * time.Minute

// Conversation is a user's chat over their library or one document.
type Conversation struct {
	ID uuid.UUID
	// DocumentID is the document the conversation is about, with its
	// title; uuid.Nil means the owner's whole library.
	DocumentID    uuid.UUID
	DocumentTitle string
	// Title is the first question, cut; "" until there is one.
	Title     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ConversationCursor marks a position in a conversation list: the
// updated_at and ID of the last conversation of the previous page.
type ConversationCursor struct {
	UpdatedAt time.Time
	ID        uuid.UUID
}

// MessageRole is who wrote a message.
type MessageRole string

// Message roles.
const (
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
)

// MessageStatus is where a message stands. A question is done when it is
// written; an answer streams until it is done, stopped or failed.
type MessageStatus string

// Message statuses.
const (
	MessageStreaming MessageStatus = "streaming"
	MessageDone      MessageStatus = "done"
	MessageStopped   MessageStatus = "stopped"
	MessageFailed    MessageStatus = "failed"
	// MessageNotFound is an answer that said the library doesn't answer: the
	// model called not_in_library, and Content.NotFound says what's missing.
	MessageNotFound MessageStatus = "not_found"
	// MessageUnsourced is an answer with text and no valid citation, and
	// without a not_in_library call: it can't be checked against the
	// library.
	MessageUnsourced MessageStatus = "unsourced"
)

// MessageContent is what a message says: text in segments and, for an
// answer, the citations its segments point at by number.
type MessageContent struct {
	Segments  []Segment  `json:"segments"`
	Citations []Citation `json:"citations"`
	// Error is why a failed answer failed, as the API names it.
	Error string `json:"error,omitempty"`
	// NotFound is what the model said the library lacks, on a not_found
	// answer.
	NotFound *NotFound `json:"not_found,omitempty"`
}

// NotFound is the model's own account of why the library doesn't answer: one
// sentence on what it lacks, and the kind of page to import.
type NotFound struct {
	Missing string `json:"missing"`
	Suggest string `json:"suggest"`
}

// Segment is a run of a message's text and the numbers of the citations
// that back it.
type Segment struct {
	Text      string `json:"text"`
	Citations []int  `json:"citations,omitempty"`
}

// Citation is blocks of the owner's document that an answer cites, and the
// text it was sent of them.
type Citation struct {
	N          int       `json:"n"`
	DocumentID uuid.UUID `json:"document_id"`
	BlockIDs   []string  `json:"block_ids"`
	CitedText  string    `json:"cited_text"`
	// Title is "Doc title > heading path".
	Title string `json:"title"`
}

// MessageUsage is what an answer cost, across all its calls to the model.
type MessageUsage struct {
	Model                    string
	InputTokens              int
	OutputTokens             int
	CacheCreationInputTokens int
	CacheReadInputTokens     int
	CostMicroUSD             int64
	// FirstTokenMS and LatencyMS are the time to the first text and to
	// the end; 0 when there was none.
	FirstTokenMS int
	LatencyMS    int
	ToolRounds   int
	// InvalidCitations counts citations dropped as pointing at nothing
	// the model was sent.
	InvalidCitations int
	// SelfMarkers counts citation markers the model wrote into its text,
	// such as [1] or [2-3]: the text is never rewritten.
	SelfMarkers int
}

// Message is one question or answer.
type Message struct {
	ID             uuid.UUID
	ConversationID uuid.UUID
	Role           MessageRole
	Status         MessageStatus
	Content        MessageContent
	Usage          MessageUsage
	CreatedAt      time.Time
}

// ChatUsage is what counts against an owner's chat quotas within a window:
// the questions asked and the input tokens their answers read, and how long
// until the oldest of each leaves it.
type ChatUsage struct {
	Questions                 int
	InputTokens               int
	UntilOldestQuestionLeaves time.Duration
	UntilOldestAnswerLeaves   time.Duration
}

// CreateConversation starts owner's conversation about document, or over
// their library when document is uuid.Nil. It returns ErrNotFound when
// document isn't owner's.
func (s *Store) CreateConversation(ctx context.Context, owner, document uuid.UUID) (Conversation, error) {
	id, err := s.q.CreateConversation(ctx, sqlcdb.CreateConversationParams{
		OwnerID: owner, DocumentID: uuid.NullUUID{UUID: document, Valid: document != uuid.Nil},
	})
	if err != nil {
		return Conversation{}, mapErr("create conversation", err)
	}
	return s.GetConversation(ctx, owner, id)
}

// GetConversation returns owner's conversation id, or ErrNotFound, also when
// it is someone else's.
func (s *Store) GetConversation(ctx context.Context, owner, id uuid.UUID) (Conversation, error) {
	row, err := s.q.GetConversation(ctx, sqlcdb.GetConversationParams{OwnerID: owner, ID: id})
	if err != nil {
		return Conversation{}, mapErr("get conversation", err)
	}
	return conversationFrom(sqlcdb.ListConversationsRow(row)), nil
}

// conversationStart is a cursor before every conversation.
var conversationStart = ConversationCursor{UpdatedAt: time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC), ID: uuid.Max}

// ListConversations returns up to limit of owner's conversations, most
// recently active first, after the cursor; nil starts at the first.
func (s *Store) ListConversations(ctx context.Context, owner uuid.UUID, after *ConversationCursor, limit int) ([]Conversation, error) {
	if after == nil {
		after = &conversationStart
	}
	rows, err := s.q.ListConversations(ctx, sqlcdb.ListConversationsParams{
		OwnerID: owner, BeforeUpdatedAt: after.UpdatedAt, BeforeID: after.ID, MaxRows: int32(min(max(limit, 0), 1000)), //nolint:gosec // clamped
	})
	if err != nil {
		return nil, mapErr("list conversations", err)
	}
	out := make([]Conversation, len(rows))
	for i, row := range rows {
		out[i] = conversationFrom(row)
	}
	return out, nil
}

// DeleteConversation deletes owner's conversation id with its messages, or
// returns ErrNotFound.
func (s *Store) DeleteConversation(ctx context.Context, owner, id uuid.UUID) error {
	n, err := s.q.DeleteConversation(ctx, sqlcdb.DeleteConversationParams{OwnerID: owner, ID: id})
	if err != nil {
		return mapErr("delete conversation", err)
	}
	if n == 0 {
		return fmt.Errorf("delete conversation: %w", ErrNotFound)
	}
	return nil
}

// LockConversation holds owner's conversation id until s's transaction
// ends, or returns ErrNotFound.
func (s *Store) LockConversation(ctx context.Context, owner, id uuid.UUID) error {
	if _, err := s.q.LockConversation(ctx, sqlcdb.LockConversationParams{OwnerID: owner, ID: id}); err != nil {
		return mapErr("lock conversation", err)
	}
	return nil
}

// TouchConversation marks owner's conversation id active now, and titles it
// title unless it has one.
func (s *Store) TouchConversation(ctx context.Context, owner, id uuid.UUID, title string) error {
	if err := s.q.TouchConversation(ctx, sqlcdb.TouchConversationParams{OwnerID: owner, ID: id, Title: title}); err != nil {
		return mapErr("touch conversation", err)
	}
	return nil
}

// AnswerRunning reports whether an answer in owner's conversation is still
// streaming, and has been for less than RunningFor.
func (s *Store) AnswerRunning(ctx context.Context, owner, conversation uuid.UUID) (bool, error) {
	n, err := s.q.CountRunningAnswers(ctx, sqlcdb.CountRunningAnswersParams{
		OwnerID: owner, ConversationID: conversation, RunningForSeconds: RunningFor.Seconds(),
	})
	if err != nil {
		return false, mapErr("count running answers", err)
	}
	return n > 0, nil
}

// AddMessage adds a message to owner's conversation.
func (s *Store) AddMessage(ctx context.Context, owner, conversation uuid.UUID, role MessageRole, status MessageStatus, content MessageContent) (Message, error) {
	data, err := marshalContent(content)
	if err != nil {
		return Message{}, err
	}
	row, err := s.q.InsertMessage(ctx, sqlcdb.InsertMessageParams{
		ConversationID: conversation, OwnerID: owner, Role: string(role), Status: string(status), Content: data,
	})
	if err != nil {
		return Message{}, mapErr("add message", err)
	}
	return messageFrom(sqlcdb.ListMessagesRow(row))
}

// ListMessages returns the messages of owner's conversation in order. An
// answer streaming for RunningFor or longer reads as failed.
func (s *Store) ListMessages(ctx context.Context, owner, conversation uuid.UUID) ([]Message, error) {
	rows, err := s.q.ListMessages(ctx, sqlcdb.ListMessagesParams{
		OwnerID: owner, ConversationID: conversation, RunningForSeconds: RunningFor.Seconds(),
	})
	if err != nil {
		return nil, mapErr("list messages", err)
	}
	out := make([]Message, len(rows))
	for i, row := range rows {
		if out[i], err = messageFrom(row); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// FinishMessage ends owner's streaming answer id with status, content and
// usage. It returns ErrNotFound when the answer is gone or already ended.
func (s *Store) FinishMessage(ctx context.Context, owner, id uuid.UUID, status MessageStatus, content MessageContent, u MessageUsage) (Message, error) {
	data, err := marshalContent(content)
	if err != nil {
		return Message{}, err
	}
	row, err := s.q.FinishMessage(ctx, sqlcdb.FinishMessageParams{
		OwnerID: owner, ID: id, Status: string(status), Content: data, Model: u.Model,
		InputTokens: clamp32(u.InputTokens), OutputTokens: clamp32(u.OutputTokens),
		CacheCreationInputTokens: clamp32(u.CacheCreationInputTokens), CacheReadInputTokens: clamp32(u.CacheReadInputTokens),
		CostMicrousd: u.CostMicroUSD, FirstTokenMs: clamp32(u.FirstTokenMS), LatencyMs: clamp32(u.LatencyMS),
		ToolRounds: clamp32(u.ToolRounds), InvalidCitations: clamp32(u.InvalidCitations), SelfMarkers: clamp32(u.SelfMarkers),
	})
	if err != nil {
		return Message{}, mapErr("finish message", err)
	}
	return messageFrom(sqlcdb.ListMessagesRow(row))
}

// ChatUsage returns owner's chat usage within window.
func (s *Store) ChatUsage(ctx context.Context, owner uuid.UUID, window time.Duration) (ChatUsage, error) {
	row, err := s.q.ChatUsage(ctx, sqlcdb.ChatUsageParams{OwnerID: owner, WindowSeconds: window.Seconds()})
	if err != nil {
		return ChatUsage{}, mapErr("chat usage", err)
	}
	return ChatUsage{
		Questions:                 int(row.Questions),
		InputTokens:               int(row.InputTokens),
		UntilOldestQuestionLeaves: time.Duration(row.SecondsUntilOldestQuestionLeaves * float64(time.Second)),
		UntilOldestAnswerLeaves:   time.Duration(row.SecondsUntilOldestAnswerLeaves * float64(time.Second)),
	}, nil
}

// SpentToday is what every user's chat answers have cost since midnight
// UTC, in micro-dollars, and how long until the next midnight.
func (s *Store) SpentToday(ctx context.Context) (microUSD int64, untilMidnight time.Duration, err error) {
	row, err := s.q.ChatSpentToday(ctx)
	if err != nil {
		return 0, 0, mapErr("chat spent today", err)
	}
	return row.SpentMicrousd, time.Duration(row.SecondsUntilMidnight * float64(time.Second)), nil
}

func conversationFrom(row sqlcdb.ListConversationsRow) Conversation {
	return Conversation{
		ID: row.ID, DocumentID: row.DocumentID.UUID, DocumentTitle: row.DocumentTitle, Title: row.Title,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func messageFrom(row sqlcdb.ListMessagesRow) (Message, error) {
	var content MessageContent
	if err := json.Unmarshal(row.Content, &content); err != nil {
		return Message{}, fmt.Errorf("message %s: %w", row.ID, err)
	}
	return Message{
		ID: row.ID, ConversationID: row.ConversationID, Role: MessageRole(row.Role), Status: MessageStatus(row.Status),
		Content: content,
		Usage: MessageUsage{
			Model: row.Model, InputTokens: int(row.InputTokens), OutputTokens: int(row.OutputTokens),
			CacheCreationInputTokens: int(row.CacheCreationInputTokens), CacheReadInputTokens: int(row.CacheReadInputTokens),
			CostMicroUSD: row.CostMicrousd, FirstTokenMS: int(row.FirstTokenMs), LatencyMS: int(row.LatencyMs),
			ToolRounds: int(row.ToolRounds), InvalidCitations: int(row.InvalidCitations), SelfMarkers: int(row.SelfMarkers),
		},
		CreatedAt: row.CreatedAt,
	}, nil
}

// marshalContent stores content with empty lists, never null.
func marshalContent(c MessageContent) ([]byte, error) {
	if c.Segments == nil {
		c.Segments = []Segment{}
	}
	if c.Citations == nil {
		c.Citations = []Citation{}
	}
	data, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("message content: %w", err)
	}
	return data, nil
}

// clamp32 is n as an int32 column holds it.
func clamp32(n int) int32 {
	return int32(min(max(n, 0), 1<<31-1)) //nolint:gosec // clamped
}
