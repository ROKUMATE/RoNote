package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/rokumate/ronote/internal/search"
	"github.com/rokumate/ronote/internal/store"
)

const (
	// quotaWindow is the rolling day the chat quotas count in.
	quotaWindow = 24 * time.Hour
	// turnTimeout bounds one answer, all its calls and searches; it is well
	// under store.RunningFor.
	turnTimeout = 120 * time.Second
	// saveTimeout bounds saving an answer once it ends.
	saveTimeout = 10 * time.Second
	// titleChars bounds a conversation's title, its first question cut.
	titleChars = 80
)

// Why a failed answer failed, as the API's ChatErrorCode names it.
const (
	CodeModelUnavailable = "model_unavailable"
	CodeModelRefused     = "model_refused"
	CodeInternal         = "internal_error"
)

var (
	// ErrDisabled means the kill switch has chat off.
	ErrDisabled = errors.New("chat is turned off")
	// ErrBusy means the conversation already has an answer being written.
	ErrBusy = errors.New("an answer is being written in this conversation")

	errStopped  = errors.New("stopped")
	errShutdown = errors.New("the server is shutting down")
)

// QuotaError means the owner has asked as much as a day allows.
// RetryAfter is how long until a question fits.
type QuotaError struct {
	RetryAfter time.Duration
}

func (e *QuotaError) Error() string {
	return fmt.Sprintf("over the chat quota; a question fits in %s", e.RetryAfter.Round(time.Second))
}

// Limits are what one user may ask in quotaWindow: questions, and the
// input tokens their answers read.
type Limits struct {
	MessagesPerDay int
	TokensPerDay   int
}

// Options are what a Service answers with.
type Options struct {
	Store    *store.Store
	Searcher *search.Searcher
	Model    Model
	// ModelName is the model's id, which prices its answers.
	ModelName string
	// Enabled is the kill switch.
	Enabled bool
	Limits  Limits
	Logger  *slog.Logger
}

// Service keeps users' conversations and writes their answers
// (docs/adr/0012-chat.md). Which answers are being written is known to this
// process alone: Stop reaches only answers it started.
type Service struct {
	o     Options
	price Price

	mu      sync.Mutex
	running map[uuid.UUID]running // by conversation
	wg      sync.WaitGroup
}

type running struct {
	owner  uuid.UUID
	cancel context.CancelCauseFunc
}

// NewService returns a Service. A model without a price is an error: its
// answers' cost would go unrecorded.
func NewService(o Options) (*Service, error) {
	price, err := PriceOf(o.ModelName)
	if err != nil {
		return nil, err
	}
	return &Service{o: o, price: price, running: map[uuid.UUID]running{}}, nil
}

// Enabled reports whether chat is on.
func (s *Service) Enabled() bool { return s.o.Enabled }

// Create starts owner's conversation about document, or over their library
// when it is uuid.Nil.
func (s *Service) Create(ctx context.Context, owner, document uuid.UUID) (store.Conversation, error) {
	return s.o.Store.CreateConversation(ctx, owner, document)
}

// List returns a page of owner's conversations, most recently active first.
func (s *Service) List(ctx context.Context, owner uuid.UUID, after *store.ConversationCursor, limit int) ([]store.Conversation, error) {
	return s.o.Store.ListConversations(ctx, owner, after, limit)
}

// Get returns owner's conversation id and its messages.
func (s *Service) Get(ctx context.Context, owner, id uuid.UUID) (store.Conversation, []store.Message, error) {
	conv, err := s.o.Store.GetConversation(ctx, owner, id)
	if err != nil {
		return store.Conversation{}, nil, err
	}
	msgs, err := s.o.Store.ListMessages(ctx, owner, id)
	return conv, msgs, err
}

// Delete deletes owner's conversation id, stopping its answer.
func (s *Service) Delete(ctx context.Context, owner, id uuid.UUID) error {
	s.cancel(owner, id, errStopped)
	return s.o.Store.DeleteConversation(ctx, owner, id)
}

// Stop ends the answer being written in owner's conversation id, if this
// process writes it; the answer is saved as stopped. It returns
// store.ErrNotFound when the conversation isn't owner's.
func (s *Service) Stop(ctx context.Context, owner, id uuid.UUID) error {
	if _, err := s.o.Store.GetConversation(ctx, owner, id); err != nil {
		return err
	}
	s.cancel(owner, id, errStopped)
	return nil
}

// StopAll ends the answers being written in owner's conversations, as when
// the document they are about is deleted with them.
func (s *Service) StopAll(owner uuid.UUID, conversations []uuid.UUID) {
	for _, id := range conversations {
		s.cancel(owner, id, errStopped)
	}
}

// MessagesLeft is how many questions owner may still ask in the window.
func (s *Service) MessagesLeft(ctx context.Context, owner uuid.UUID) (int, error) {
	u, err := s.o.Store.ChatUsage(ctx, owner, quotaWindow)
	if err != nil {
		return 0, err
	}
	return max(0, s.o.Limits.MessagesPerDay-u.Questions), nil
}

// Shutdown waits for the answers being written, then stops those still
// going when ctx ends and waits for them to be saved.
func (s *Service) Shutdown(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
	}
	s.mu.Lock()
	for _, r := range s.running {
		r.cancel(errShutdown)
	}
	s.mu.Unlock()
	<-done
	return ctx.Err()
}

func (s *Service) cancel(owner, conversation uuid.UUID, cause error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.running[conversation]; ok && r.owner == owner {
		r.cancel(cause)
	}
}

// Update is what an answer says as it is written: TextUpdate,
// CitationUpdate and SearchUpdate, then DoneUpdate, or FailUpdate when it
// couldn't be saved.
type Update interface{ update() }

// TextUpdate is more text of segment Segment.
type TextUpdate struct {
	Segment int
	Text    string
}

// CitationUpdate is a citation that backs segment Segment.
type CitationUpdate struct {
	Segment  int
	Citation store.Citation
}

// SearchUpdate says the model searches the library for Query.
type SearchUpdate struct{ Query string }

// DoneUpdate is the answer as saved, and the questions left today.
type DoneUpdate struct {
	Message      store.Message
	MessagesLeft int
}

// FailUpdate says the answer couldn't be saved, with a Code* why.
type FailUpdate struct{ Code string }

func (TextUpdate) update()     {}
func (CitationUpdate) update() {}
func (SearchUpdate) update()   {}
func (DoneUpdate) update()     {}
func (FailUpdate) update()     {}

// Answer is an answer being written. Its updates arrive in order; the
// channel closes after the last.
type Answer struct {
	// MessageID is the answer's message.
	MessageID uuid.UUID
	updates   chan Update
	gone      chan struct{}
	leave     sync.Once
}

// Updates returns the answer's updates.
func (a *Answer) Updates() <-chan Update { return a.updates }

// Leave stops the updates: nobody is listening. The answer goes on and is
// saved.
func (a *Answer) Leave() { a.leave.Do(func() { close(a.gone) }) }

func (a *Answer) send(u Update) {
	select {
	case a.updates <- u:
	case <-a.gone:
	}
}

// Ask starts the answer to question in owner's conversation. It refuses
// with ErrDisabled, store.ErrNotFound, *QuotaError or ErrBusy. The answer
// is written apart from ctx: a caller that goes away leaves it to be saved.
func (s *Service) Ask(ctx context.Context, owner, conversation uuid.UUID, question string) (*Answer, error) {
	if !s.o.Enabled {
		return nil, ErrDisabled
	}
	var conv store.Conversation
	var earlier []store.Message
	var answer store.Message
	err := s.o.Store.InTx(ctx, func(tx *store.Store, _ pgx.Tx) error {
		if err := tx.LockOwner(ctx, owner); err != nil {
			return err
		}
		u, err := tx.ChatUsage(ctx, owner, quotaWindow)
		if err != nil {
			return err
		}
		switch {
		case u.Questions >= s.o.Limits.MessagesPerDay:
			return &QuotaError{RetryAfter: u.UntilOldestQuestionLeaves}
		case u.InputTokens >= s.o.Limits.TokensPerDay:
			return &QuotaError{RetryAfter: u.UntilOldestAnswerLeaves}
		}
		if err := tx.LockConversation(ctx, owner, conversation); err != nil {
			return err
		}
		if conv, err = tx.GetConversation(ctx, owner, conversation); err != nil {
			return err
		}
		if busy, err := tx.AnswerRunning(ctx, owner, conversation); err != nil {
			return err
		} else if busy {
			return ErrBusy
		}
		if earlier, err = tx.ListMessages(ctx, owner, conversation); err != nil {
			return err
		}
		asked := store.MessageContent{Segments: []store.Segment{{Text: question}}}
		if _, err := tx.AddMessage(ctx, owner, conversation, store.RoleUser, store.MessageDone, asked); err != nil {
			return err
		}
		if answer, err = tx.AddMessage(ctx, owner, conversation, store.RoleAssistant, store.MessageStreaming, store.MessageContent{}); err != nil {
			return err
		}
		return tx.TouchConversation(ctx, owner, conversation, title(question))
	})
	if err != nil {
		return nil, err
	}

	runCtx, cancel := context.WithCancelCause(context.WithoutCancel(ctx))
	timed, stopTimer := context.WithTimeout(runCtx, turnTimeout)
	a := &Answer{MessageID: answer.ID, updates: make(chan Update, 64), gone: make(chan struct{})}
	s.mu.Lock()
	s.running[conversation] = running{owner: owner, cancel: cancel}
	s.mu.Unlock()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer close(a.updates)
		defer func() {
			stopTimer()
			cancel(nil)
			s.mu.Lock()
			delete(s.running, conversation)
			s.mu.Unlock()
		}()
		t := &turn{lib: library{searcher: s.o.Searcher, owner: owner, document: conv.DocumentID}, start: time.Now(), answer: a}
		err := s.converse(timed, t, exchanges(earlier), question)
		s.finish(timed, t, owner, conversation, err)
	}()
	return a, nil
}

// turn is an answer as it is written.
type turn struct {
	lib      library
	cite     citer
	answer   *Answer
	segments []store.Segment
	usage    Usage
	rounds   int
	start    time.Time
	// firstText is how long the first text took.
	firstText time.Duration
	refused   bool
	// notFound is what the model said the library lacks, when it called
	// not_in_library.
	notFound *store.NotFound
}

// converse grounds the question in the conversation's library and calls
// the model until it answers or says the library doesn't, with up to
// maxToolRounds searches between. The call after the last search offers
// only not_in_library, so the model answers with what it has or says so.
func (s *Service) converse(ctx context.Context, t *turn, earlier []exchange, question string) error {
	passages, err := t.lib.search(ctx, question, false)
	if err != nil {
		return err
	}
	t.cite.sent = passages
	asked := make([]Block, 0, len(passages)+1)
	for _, p := range passages {
		asked = append(asked, p.result())
	}
	req := Request{
		System:    systemPrompt,
		Tools:     libraryTools,
		Messages:  append(history(earlier, historyRoom(question, passages)), Message{Role: User, Blocks: append(asked, Text{Text: question})}),
		MaxTokens: maxTokens,
	}
	next := firstCallTokens(req)
	for {
		if next > inputCap {
			return fmt.Errorf("a call of about %d input tokens would pass the cap of %d", next, inputCap)
		}
		last := t.rounds >= maxToolRounds
		req.Tools = libraryTools
		if last {
			req.Tools = []Tool{notFoundTool}
		}
		reply, err := s.o.Model.Stream(ctx, req, t.hear)
		t.usage = t.usage.Add(reply.Usage)
		if err != nil {
			return err
		}
		if reply.Stop == StopRefusal {
			t.refused = true
			return nil
		}
		// A valid not_in_library call ends the turn, whatever else the reply
		// calls; an invalid one is told why.
		unshown := map[string]string{}
		for _, use := range reply.ToolUses {
			if use.Name != notFoundName {
				continue
			}
			nf, why := parseNotFound(use)
			if why == "" {
				t.notFound = &nf
				return nil
			}
			unshown[use.ID] = why
		}
		if reply.Stop != StopToolUse || len(reply.ToolUses) == 0 || last {
			return nil
		}
		t.rounds++
		results := make([]Block, 0, len(reply.ToolUses))
		// What the call read and wrote, as the API counts it, is all sent
		// again, then the results.
		next = reply.Usage.InputTokens + reply.Usage.CacheCreationInputTokens + reply.Usage.CacheReadInputTokens + reply.Usage.OutputTokens
		for _, use := range reply.ToolUses {
			if why, bad := unshown[use.ID]; bad {
				result := ToolResult{ToolUseID: use.ID, Text: why, IsError: true}
				results = append(results, result)
				next += blockTokens(result)
				continue
			}
			if q := queryOf(use); q != "" {
				t.answer.send(SearchUpdate{Query: q})
			}
			result, found, err := t.lib.run(ctx, use)
			if err != nil {
				return err
			}
			// Search results count in request order: these follow every
			// result sent before them.
			t.cite.sent = append(t.cite.sent, found...)
			results = append(results, result)
			next += blockTokens(result)
		}
		req.Messages = append(req.Messages, Message{Role: Assistant, Blocks: reply.Content}, Message{Role: User, Blocks: results})
	}
}

// hear takes in what a reply streams: text into segments, a block of text
// each, and the valid citations of each.
func (t *turn) hear(ev Event) {
	switch e := ev.(type) {
	case TextStart:
		t.segments = append(t.segments, store.Segment{})
	case TextDelta:
		if len(t.segments) == 0 {
			t.segments = append(t.segments, store.Segment{})
		}
		if t.firstText == 0 {
			t.firstText = time.Since(t.start)
		}
		i := len(t.segments) - 1
		t.segments[i].Text += e.Text
		t.answer.send(TextUpdate{Segment: i, Text: e.Text})
	case Cited:
		n := t.cite.cite(e)
		if n == 0 || len(t.segments) == 0 {
			return
		}
		i := len(t.segments) - 1
		if slices.Contains(t.segments[i].Citations, n) {
			return
		}
		t.segments[i].Citations = append(t.segments[i].Citations, n)
		t.answer.send(CitationUpdate{Segment: i, Citation: t.cite.cited[n-1]})
	}
}

// finish saves the answer as it ended, and sends it.
func (s *Service) finish(ctx context.Context, t *turn, owner, conversation uuid.UUID, err error) {
	status, code := store.MessageDone, ""
	switch {
	case errors.Is(context.Cause(ctx), errStopped):
		status = store.MessageStopped
	case err == nil && t.refused:
		status, code = store.MessageFailed, CodeModelRefused
	case err == nil && t.notFound != nil:
		status = store.MessageNotFound
	case err == nil && len(t.cite.cited) == 0:
		// Text with no valid citation, or none at all: it can't be checked
		// against the library, which is not the same as saying it isn't there.
		status = store.MessageUnsourced
	case err == nil:
	case errors.Is(err, ErrUnavailable) || errors.Is(err, context.DeadlineExceeded):
		status, code = store.MessageFailed, CodeModelUnavailable
	default:
		status, code = store.MessageFailed, CodeInternal
	}
	if err != nil && status == store.MessageFailed {
		s.o.Logger.LogAttrs(ctx, slog.LevelError, "answer failed",
			slog.String("conversation_id", conversation.String()), slog.String("error", err.Error()))
	}
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), saveTimeout)
	defer cancel()
	content := store.MessageContent{Segments: t.segments, Citations: t.cite.cited, Error: code}
	if status == store.MessageNotFound {
		content.NotFound = t.notFound
	}
	usage := store.MessageUsage{
		Model: s.o.ModelName, InputTokens: t.usage.InputTokens, OutputTokens: t.usage.OutputTokens,
		CacheCreationInputTokens: t.usage.CacheCreationInputTokens, CacheReadInputTokens: t.usage.CacheReadInputTokens,
		CostMicroUSD: s.price.Cost(t.usage), FirstTokenMS: int(t.firstText.Milliseconds()), LatencyMS: int(time.Since(t.start).Milliseconds()),
		ToolRounds: t.rounds, InvalidCitations: t.cite.invalid, SelfMarkers: countMarkers(text(content)),
	}
	msg, err := s.o.Store.FinishMessage(saveCtx, owner, t.answer.MessageID, status, content, usage)
	if err != nil {
		// Deleted with its conversation, or the database is gone.
		s.o.Logger.LogAttrs(saveCtx, slog.LevelWarn, "answer not saved",
			slog.String("conversation_id", conversation.String()), slog.String("error", err.Error()))
		t.answer.send(FailUpdate{Code: CodeInternal})
		return
	}
	left, err := s.MessagesLeft(saveCtx, owner)
	if err != nil {
		s.o.Logger.LogAttrs(saveCtx, slog.LevelWarn, "chat usage unread", slog.String("error", err.Error()))
	}
	s.o.Logger.LogAttrs(saveCtx, slog.LevelInfo, "answer finished",
		slog.String("conversation_id", conversation.String()), slog.String("status", string(status)),
		slog.String("model", usage.Model), slog.Int("input_tokens", usage.InputTokens), slog.Int("output_tokens", usage.OutputTokens),
		slog.Int("cache_read_input_tokens", usage.CacheReadInputTokens), slog.Int64("cost_microusd", usage.CostMicroUSD),
		slog.Int("first_token_ms", usage.FirstTokenMS), slog.Int("latency_ms", usage.LatencyMS),
		slog.Int("tool_rounds", usage.ToolRounds), slog.Int("citations", len(content.Citations)), slog.Int("invalid_citations", usage.InvalidCitations))
	t.answer.send(DoneUpdate{Message: msg, MessagesLeft: left})
}

// exchanges are the earlier questions with an answer to send again: one
// done, stopped, not found or unsourced, with text.
func exchanges(msgs []store.Message) []exchange {
	var out []exchange
	for i := 0; i+1 < len(msgs); i++ {
		q, a := msgs[i], msgs[i+1]
		if q.Role != store.RoleUser || a.Role != store.RoleAssistant || !slices.Contains([]store.MessageStatus{store.MessageDone, store.MessageStopped, store.MessageNotFound, store.MessageUnsourced}, a.Status) {
			continue
		}
		if answer := answerText(a); answer != "" {
			out = append(out, exchange{question: text(q.Content), answer: answer})
		}
	}
	return out
}

// answerText is what a later turn is told an answer said: for a not_found
// answer, what the model said the library lacks.
func answerText(a store.Message) string {
	if a.Status == store.MessageNotFound && a.Content.NotFound != nil {
		return notFoundText(*a.Content.NotFound)
	}
	return text(a.Content)
}

func text(c store.MessageContent) string {
	var b strings.Builder
	for _, s := range c.Segments {
		b.WriteString(s.Text)
	}
	return strings.TrimSpace(b.String())
}

// title is a question's first line, cut at a word to titleChars.
func title(question string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(question), "\n")
	if utf8.RuneCountInString(line) <= titleChars {
		return line
	}
	cut := string([]rune(line)[:titleChars])
	if i := strings.LastIndexByte(cut, ' '); i > titleChars/2 {
		cut = cut[:i]
	}
	return strings.TrimSpace(cut) + "…"
}

// queryOf is a search_library call's query, or "".
func queryOf(use ToolUse) string {
	var in toolInput
	if use.Name != toolName || json.Unmarshal(use.Input, &in) != nil {
		return ""
	}
	return strings.TrimSpace(in.Query)
}
