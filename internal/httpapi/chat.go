package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/chat"
	"github.com/rokumate/ronote/internal/httpapi/gen"
	"github.com/rokumate/ronote/internal/store"
)

// chatOff answers every conversations route with 503 chat_disabled while
// the kill switch has chat off, or there is no chat to answer with
// (docs/adr/0012-chat.md).
func chatOff(c *chat.Service, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (c == nil || !c.Enabled()) && strings.HasPrefix(r.URL.Path, "/api/v1/conversations") {
			writeError(w, r, http.StatusServiceUnavailable, gen.ErrorCodeChatDisabled, "chat is turned off")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s apiServer) ListConversations(ctx context.Context, req gen.ListConversationsRequestObject) (gen.ListConversationsResponseObject, error) {
	limit := defaultPageSize
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	if limit < 1 || limit > maxPageSize {
		return gen.ListConversations400JSONResponse(apiError(ctx, gen.ErrorCodeInvalidParameter, "limit must be between 1 and 100")), nil
	}
	var after *store.ConversationCursor
	if req.Params.Cursor != nil {
		at, id, err := decodeCursor(*req.Params.Cursor)
		if err != nil {
			return gen.ListConversations400JSONResponse(apiError(ctx, gen.ErrorCodeInvalidCursor, "the cursor is not one this API made")), nil
		}
		after = &store.ConversationCursor{UpdatedAt: at, ID: id}
	}
	owner := userFrom(ctx)
	// One more than asked tells whether another page follows.
	convs, err := s.chat.List(ctx, owner, after, limit+1)
	if err != nil {
		return nil, err
	}
	left, err := s.chat.MessagesLeft(ctx, owner)
	if err != nil {
		return nil, err
	}
	out := gen.ListConversations200JSONResponse{Conversations: []gen.Conversation{}, MessagesLeftToday: left}
	if len(convs) > limit {
		convs = convs[:limit]
		last := convs[limit-1]
		next := encodeCursor(last.UpdatedAt, last.ID)
		out.NextCursor = &next
	}
	for _, c := range convs {
		out.Conversations = append(out.Conversations, conversationJSON(c))
	}
	return out, nil
}

func (s apiServer) CreateConversation(ctx context.Context, req gen.CreateConversationRequestObject) (gen.CreateConversationResponseObject, error) {
	if req.Body == nil {
		return gen.CreateConversation400JSONResponse(apiError(ctx, gen.ErrorCodeInvalidRequest, "a JSON body is required, {} for the whole library")), nil
	}
	document := uuid.Nil
	if req.Body.DocumentID != nil {
		document = *req.Body.DocumentID
	}
	conv, err := s.chat.Create(ctx, userFrom(ctx), document)
	if errors.Is(err, store.ErrNotFound) {
		return gen.CreateConversation404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(apiError(ctx, gen.ErrorCodeNotFound, "no such document"))}, nil
	}
	if err != nil {
		return nil, err
	}
	return gen.CreateConversation201JSONResponse(conversationJSON(conv)), nil
}

func (s apiServer) GetConversation(ctx context.Context, req gen.GetConversationRequestObject) (gen.GetConversationResponseObject, error) {
	conv, msgs, err := s.chat.Get(ctx, userFrom(ctx), req.ID)
	if errors.Is(err, store.ErrNotFound) {
		return gen.GetConversation404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(apiError(ctx, gen.ErrorCodeNotFound, "no such conversation"))}, nil
	}
	if err != nil {
		return nil, err
	}
	out := gen.GetConversation200JSONResponse{Conversation: conversationJSON(conv), Messages: make([]gen.Message, len(msgs))}
	for i, m := range msgs {
		out.Messages[i] = messageJSON(m)
	}
	return out, nil
}

func (s apiServer) DeleteConversation(ctx context.Context, req gen.DeleteConversationRequestObject) (gen.DeleteConversationResponseObject, error) {
	err := s.chat.Delete(ctx, userFrom(ctx), req.ID)
	if errors.Is(err, store.ErrNotFound) {
		return gen.DeleteConversation404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(apiError(ctx, gen.ErrorCodeNotFound, "no such conversation"))}, nil
	}
	if err != nil {
		return nil, err
	}
	return gen.DeleteConversation204Response{}, nil
}

func (s apiServer) StopConversation(ctx context.Context, req gen.StopConversationRequestObject) (gen.StopConversationResponseObject, error) {
	err := s.chat.Stop(ctx, userFrom(ctx), req.ID)
	if errors.Is(err, store.ErrNotFound) {
		return gen.StopConversation404JSONResponse{NotFoundJSONResponse: gen.NotFoundJSONResponse(apiError(ctx, gen.ErrorCodeNotFound, "no such conversation"))}, nil
	}
	if err != nil {
		return nil, err
	}
	return gen.StopConversation204Response{}, nil
}

// sendMessage asks a question in a conversation and streams its answer as
// server-sent events, a heartbeat comment every every. The answer is
// written apart from the request, so it goes on, and is saved, when the
// client leaves or the server begins to shut down.
func sendMessage(o Options, every time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		id, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			writeError(w, r, http.StatusBadRequest, gen.ErrorCodeInvalidParameter, "id is not a UUID")
			return
		}
		var body gen.SendMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			badBody(w, r, err, "a JSON body with the question's text is required")
			return
		}
		text := strings.TrimSpace(body.Text)
		if text == "" || utf8.RuneCountInString(text) > chat.MaxQuestion {
			writeError(w, r, http.StatusBadRequest, gen.ErrorCodeInvalidRequest, fmt.Sprintf("text must be 1 to %d characters", chat.MaxQuestion))
			return
		}
		answer, err := o.Chat.Ask(ctx, userFrom(ctx), id, text)
		var quota *chat.QuotaError
		switch {
		case errors.Is(err, store.ErrNotFound):
			writeError(w, r, http.StatusNotFound, gen.ErrorCodeNotFound, "no such conversation")
			return
		case errors.Is(err, chat.ErrBusy):
			writeError(w, r, http.StatusConflict, gen.ErrorCodeConversationBusy, err.Error())
			return
		case errors.As(err, &quota):
			w.Header().Set("Retry-After", strconv.Itoa(retrySeconds(quota.RetryAfter)))
			writeError(w, r, http.StatusTooManyRequests, gen.ErrorCodeQuotaExceeded, err.Error())
			return
		case errors.Is(err, chat.ErrDisabled):
			writeError(w, r, http.StatusServiceUnavailable, gen.ErrorCodeChatDisabled, "chat is turned off")
			return
		case err != nil:
			internalError(o.Logger, w, r, err)
			return
		}
		defer answer.Leave()

		rc := http.NewResponseController(w)
		// The server's write timeout is for ordinary responses; an answer
		// takes as long as it takes.
		_ = rc.SetWriteDeadline(time.Time{})
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		_ = rc.Flush()
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-o.Done:
				return
			case <-ticker.C:
				if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil || rc.Flush() != nil {
					return
				}
			case u, ok := <-answer.Updates():
				if !ok || writeUpdate(w, rc, u) != nil {
					return
				}
			}
		}
	}
}

// writeUpdate writes an answer's update as its event.
func writeUpdate(w http.ResponseWriter, rc *http.ResponseController, u chat.Update) error {
	switch u := u.(type) {
	case chat.TextUpdate:
		return writeEvent(w, rc, "delta", gen.ChatDelta{Segment: u.Segment, Text: u.Text})
	case chat.CitationUpdate:
		return writeEvent(w, rc, "citation", gen.ChatCitation{Segment: u.Segment, Citation: citationJSON(u.Citation)})
	case chat.SearchUpdate:
		return writeEvent(w, rc, "tool", gen.ChatTool{Query: u.Query})
	case chat.DoneUpdate:
		if code := u.Message.Content.Error; code != "" {
			if err := writeEvent(w, rc, "error", gen.ChatError{Code: gen.ChatErrorCode(code)}); err != nil {
				return err
			}
		}
		return writeEvent(w, rc, "done", gen.ChatDone{Message: messageJSON(u.Message), MessagesLeftToday: u.MessagesLeft})
	case chat.FailUpdate:
		return writeEvent(w, rc, "error", gen.ChatError{Code: gen.ChatErrorCode(u.Code)})
	}
	return nil
}

// conversationJSON and messageJSON give times in UTC, as importJSON does.
func conversationJSON(c store.Conversation) gen.Conversation {
	out := gen.Conversation{ID: c.ID, Title: c.Title, CreatedAt: c.CreatedAt.UTC(), UpdatedAt: c.UpdatedAt.UTC()}
	if c.DocumentID != uuid.Nil {
		out.DocumentID, out.DocumentTitle = &c.DocumentID, &c.DocumentTitle
	}
	return out
}

func messageJSON(m store.Message) gen.Message {
	out := gen.Message{
		ID: m.ID, Role: gen.MessageRole(m.Role), Status: gen.MessageStatus(m.Status), CreatedAt: m.CreatedAt.UTC(),
		Segments: make([]gen.MessageSegment, len(m.Content.Segments)), Citations: make([]gen.Citation, len(m.Content.Citations)),
	}
	for i, s := range m.Content.Segments {
		out.Segments[i] = gen.MessageSegment{Text: s.Text, Citations: append([]int{}, s.Citations...)}
	}
	for i, c := range m.Content.Citations {
		out.Citations[i] = citationJSON(c)
	}
	if m.Content.Error != "" {
		code := gen.ChatErrorCode(m.Content.Error)
		out.Error = &code
	}
	if n := m.Content.NotFound; n != nil {
		out.NotFound = &gen.ChatNotFound{Missing: n.Missing, Suggest: n.Suggest}
	}
	if m.Role == store.RoleAssistant && m.Status != store.MessageStreaming {
		u := m.Usage
		out.Usage = &gen.ChatUsage{
			Model: u.Model, InputTokens: u.InputTokens, OutputTokens: u.OutputTokens,
			CacheCreationInputTokens: u.CacheCreationInputTokens, CacheReadInputTokens: u.CacheReadInputTokens,
			CostMicrousd: u.CostMicroUSD, FirstTokenMs: u.FirstTokenMS, LatencyMs: u.LatencyMS,
		}
	}
	return out
}

func citationJSON(c store.Citation) gen.Citation {
	return gen.Citation{N: c.N, DocumentID: c.DocumentID, BlockIds: c.BlockIDs, CitedText: c.CitedText, Title: c.Title}
}
