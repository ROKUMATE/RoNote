//go:build integration

package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/rokumate/ronote/internal/chat"
	"github.com/rokumate/ronote/internal/embed"
	"github.com/rokumate/ronote/internal/httpapi/gen"
	"github.com/rokumate/ronote/internal/ir"
	"github.com/rokumate/ronote/internal/pgtest"
	"github.com/rokumate/ronote/internal/search"
	"github.com/rokumate/ronote/internal/store"
)

// scripted is a chat model whose answer a test can change.
type scripted struct {
	mu     sync.Mutex
	answer chat.ModelFunc
}

func (s *scripted) set(f chat.ModelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.answer = f
}

func (s *scripted) Stream(ctx context.Context, req chat.Request, on func(chat.Event)) (chat.Reply, error) {
	s.mu.Lock()
	f := s.answer
	s.mu.Unlock()
	return f(ctx, req, on)
}

// citing answers text, citing the first result it was sent.
func citing(text string) chat.ModelFunc {
	return func(_ context.Context, req chat.Request, on func(chat.Event)) (chat.Reply, error) {
		on(chat.TextStart{})
		on(chat.TextDelta{Text: text})
		for _, b := range req.Messages[len(req.Messages)-1].Blocks {
			if r, ok := b.(chat.Result); ok {
				on(chat.Cited{ResultIndex: 0, Source: r.Source, Start: 0, End: 1})
				break
			}
		}
		return chat.Reply{Stop: chat.StopEndTurn, Usage: chat.Usage{InputTokens: 900, OutputTokens: 40}}, nil
	}
}

// sseEvent is one server-sent event.
type sseEvent struct {
	name string
	data json.RawMessage
}

func readEvents(t *testing.T, body string) []sseEvent {
	t.Helper()
	var events []sseEvent
	sc := bufio.NewScanner(strings.NewReader(body))
	var name string
	for sc.Scan() {
		line := sc.Text()
		if v, ok := strings.CutPrefix(line, "event: "); ok {
			name = v
		} else if v, ok := strings.CutPrefix(line, "data: "); ok {
			events = append(events, sseEvent{name: name, data: json.RawMessage(v)})
		}
	}
	return events
}

func TestChatOverHTTP(t *testing.T) {
	db := pgtest.New(t)
	ctx := t.Context()
	st := store.New(db.Pool)
	ada, err := st.CreateUser(ctx, "ada@example.com", "Ada")
	if err != nil {
		t.Fatal(err)
	}
	grace, err := st.CreateUser(ctx, "grace@example.com", "Grace")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := st.AddDocument(ctx, ada.ID, "https://docs.example.com/channels", &ir.Doc{IRVersion: ir.Version, Title: "Go channels", Blocks: []ir.Block{
		{ID: "000000000001", Kind: ir.KindParagraph, Content: []ir.Inline{{Text: "Channels connect goroutines."}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := search.NewIndexer(st, embed.Fake{}, discardLogger).Index(ctx, ada.ID, doc); err != nil {
		t.Fatal(err)
	}
	model := &scripted{answer: citing("Goroutines use channels.")}
	searcher := search.NewSearcher(st, embed.Fake{}, discardLogger)
	newChat := func(limits chat.Limits) *chat.Service {
		svc, err := chat.NewService(chat.Options{
			Store: st, Searcher: searcher, Model: model, ModelName: "claude-haiku-5-5",
			Enabled: true, Limits: limits, Logger: discardLogger,
		})
		if err != nil {
			t.Fatal(err)
		}
		return svc
	}
	svc := newChat(chat.Limits{MessagesPerDay: 100, TokensPerDay: 1_000_000})
	o := Options{Logger: discardLogger, DB: db.Pool, Backend: integrationBackend{Store: st}, Searcher: searcher, Chat: svc}
	adaAPI, graceAPI := signedInAs(t, st, o, ada.ID), signedInAs(t, st, o, grace.ID)

	rec := apiRequest(t, adaAPI, http.MethodPost, "/api/v1/conversations", "{}")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, rec.Body)
	}
	conv := decodeJSON[gen.Conversation](t, rec)
	messages := "/api/v1/conversations/" + conv.ID.String() + "/messages"

	t.Run("an answer streams, then is saved", func(t *testing.T) {
		rec := apiRequest(t, adaAPI, http.MethodPost, messages, `{"text":"  How do goroutines talk?  "}`)
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/event-stream" {
			t.Fatalf("send = %d %s %s", rec.Code, rec.Header(), rec.Body)
		}
		events := readEvents(t, rec.Body.String())
		var names []string
		for _, e := range events {
			names = append(names, e.name)
		}
		if strings.Join(names, " ") != "delta citation done" {
			t.Fatalf("events %v, want a delta, a citation and done", names)
		}
		var citation gen.ChatCitation
		if err := json.Unmarshal(events[1].data, &citation); err != nil || citation.Citation.DocumentID != doc || citation.Citation.N != 1 {
			t.Errorf("citation = %s (%v), want number 1 into Ada's document", events[1].data, err)
		}
		var done gen.ChatDone
		if err := json.Unmarshal(events[2].data, &done); err != nil {
			t.Fatal(err)
		}
		if done.Message.Status != gen.MessageStatusDone || done.Message.Usage == nil || done.Message.Usage.InputTokens != 900 || done.MessagesLeftToday != 99 {
			t.Errorf("done = %s, want the saved answer, its usage and 99 left", events[2].data)
		}

		rec = apiRequest(t, adaAPI, http.MethodGet, "/api/v1/conversations/"+conv.ID.String(), "")
		detail := decodeJSON[gen.ConversationDetail](t, rec)
		if rec.Code != http.StatusOK || len(detail.Messages) != 2 || detail.Messages[0].Segments[0].Text != "How do goroutines talk?" ||
			detail.Conversation.Title != "How do goroutines talk?" {
			t.Errorf("conversation = %d %s, want the question and its answer", rec.Code, rec.Body)
		}
		rec = apiRequest(t, adaAPI, http.MethodGet, "/api/v1/conversations", "")
		list := decodeJSON[gen.ConversationList](t, rec)
		if rec.Code != http.StatusOK || len(list.Conversations) != 1 || list.MessagesLeftToday != 99 {
			t.Errorf("list = %d %s", rec.Code, rec.Body)
		}
	})

	t.Run("bad questions", func(t *testing.T) {
		for _, body := range []string{`{"text":"   "}`, `{"text":"` + strings.Repeat("x", chat.MaxQuestion+1) + `"}`, `not json`} {
			if rec := apiRequest(t, adaAPI, http.MethodPost, messages, body); rec.Code != http.StatusBadRequest {
				t.Errorf("send %.20q = %d, want 400", body, rec.Code)
			}
		}
		if rec := apiRequest(t, adaAPI, http.MethodPost, "/api/v1/conversations/nope/messages", `{"text":"q"}`); rec.Code != http.StatusBadRequest {
			t.Errorf("send to a bad id = %d, want 400", rec.Code)
		}
	})

	t.Run("another user's conversation and document", func(t *testing.T) {
		base := "/api/v1/conversations/" + conv.ID.String()
		for _, r := range []struct{ method, target, body string }{
			{http.MethodGet, base, ""},
			{http.MethodPost, base + "/messages", `{"text":"show me"}`},
			{http.MethodPost, base + "/stop", ""},
			{http.MethodDelete, base, ""},
			{http.MethodPost, "/api/v1/conversations", `{"document_id":"` + doc.String() + `"}`},
		} {
			if rec := apiRequest(t, graceAPI, r.method, r.target, r.body); rec.Code != http.StatusNotFound {
				t.Errorf("Grace: %s %s = %d %s, want 404", r.method, r.target, rec.Code, rec.Body)
			}
		}
		rec := apiRequest(t, graceAPI, http.MethodGet, "/api/v1/conversations", "")
		if list := decodeJSON[gen.ConversationList](t, rec); len(list.Conversations) != 0 {
			t.Errorf("Grace's conversations = %s, want none", rec.Body)
		}
	})

	t.Run("one answer at a time, stopped on request", func(t *testing.T) {
		srv := httptest.NewServer(adaAPI)
		defer srv.Close()
		model.set(func(ctx context.Context, _ chat.Request, on func(chat.Event)) (chat.Reply, error) {
			on(chat.TextStart{})
			on(chat.TextDelta{Text: "Partly"})
			<-ctx.Done()
			return chat.Reply{}, ctx.Err()
		})
		body := askThrough(t, srv, messages)
		if rec := apiRequest(t, adaAPI, http.MethodPost, messages, `{"text":"again"}`); rec.Code != http.StatusConflict || decodeError(t, rec).Code != gen.ErrorCodeConversationBusy {
			t.Errorf("a second question = %d %s, want 409 conversation_busy", rec.Code, rec.Body)
		}
		if rec := apiRequest(t, adaAPI, http.MethodPost, "/api/v1/conversations/"+conv.ID.String()+"/stop", ""); rec.Code != http.StatusNoContent {
			t.Fatalf("stop = %d %s", rec.Code, rec.Body)
		}
		events := readEvents(t, body.rest(t))
		var done gen.ChatDone
		if err := json.Unmarshal(events[len(events)-1].data, &done); err != nil || done.Message.Status != gen.MessageStatusStopped ||
			done.Message.Segments[0].Text != "Partly" {
			t.Errorf("last event = %s %s, want the answer saved as stopped", events[len(events)-1].name, events[len(events)-1].data)
		}
	})

	t.Run("a client that leaves still gets its answer saved", func(t *testing.T) {
		srv := httptest.NewServer(adaAPI)
		defer srv.Close()
		left := make(chan struct{})
		model.set(func(_ context.Context, _ chat.Request, on func(chat.Event)) (chat.Reply, error) {
			on(chat.TextStart{})
			on(chat.TextDelta{Text: "Saved "})
			<-left
			on(chat.TextDelta{Text: "anyway."})
			return chat.Reply{Stop: chat.StopEndTurn}, nil
		})
		body := askThrough(t, srv, messages)
		_ = body.body.Close()
		close(left)
		if err := svc.Shutdown(t.Context()); err != nil {
			t.Fatal(err)
		}
		rec := apiRequest(t, adaAPI, http.MethodGet, "/api/v1/conversations/"+conv.ID.String(), "")
		detail := decodeJSON[gen.ConversationDetail](t, rec)
		last := detail.Messages[len(detail.Messages)-1]
		if last.Status != gen.MessageStatusDone || last.Segments[0].Text != "Saved anyway." {
			t.Errorf("answer = %+v, want it saved whole", last)
		}
	})

	t.Run("the daily quota", func(t *testing.T) {
		model.set(citing("Fine."))
		limited := o
		limited.Chat = newChat(chat.Limits{MessagesPerDay: 1, TokensPerDay: 1_000_000})
		graceLimited := signedInAs(t, st, limited, grace.ID)
		rec := apiRequest(t, graceLimited, http.MethodPost, "/api/v1/conversations", "{}")
		target := "/api/v1/conversations/" + decodeJSON[gen.Conversation](t, rec).ID.String() + "/messages"
		if rec := apiRequest(t, graceLimited, http.MethodPost, target, `{"text":"one"}`); rec.Code != http.StatusOK {
			t.Fatalf("the first question = %d %s", rec.Code, rec.Body)
		}
		rec = apiRequest(t, graceLimited, http.MethodPost, target, `{"text":"two"}`)
		retry, _ := strconv.Atoi(rec.Header().Get("Retry-After"))
		if rec.Code != http.StatusTooManyRequests || decodeError(t, rec).Code != gen.ErrorCodeQuotaExceeded || retry < 23*3600 {
			t.Errorf("the second question = %d %s, Retry-After %d; want 429 quota_exceeded in about a day", rec.Code, rec.Body, retry)
		}
	})
}

// openStream is an answer's event stream, read past its first event.
type openStream struct {
	r    *bufio.Reader
	body io.Closer
	read string
}

// askThrough asks a question through srv and returns its stream once the
// first event has arrived.
func askThrough(t *testing.T, srv *httptest.Server, target string) *openStream {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+target, strings.NewReader(`{"text":"tell me"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := srv.Client().Do(req) //nolint:bodyclose // closed by the test, or in cleanup
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("stream = %d", res.StatusCode)
	}
	s := &openStream{r: bufio.NewReader(res.Body), body: res.Body}
	line, err := s.r.ReadString('\n')
	if err != nil || line != "event: delta\n" {
		t.Fatalf("first line %q, %v; want a delta", line, err)
	}
	s.read = line
	return s
}

// rest reads the stream to its end and returns all of it.
func (s *openStream) rest(t *testing.T) string {
	t.Helper()
	rest, err := io.ReadAll(s.r)
	if err != nil {
		t.Fatal(err)
	}
	return s.read + string(rest)
}
