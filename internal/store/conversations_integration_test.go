//go:build integration

package store

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestConversations(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	doc := f.document(t, f.ada)

	library, err := f.s.CreateConversation(ctx, f.ada.ID, uuid.Nil)
	if err != nil {
		t.Fatal(err)
	}
	if library.DocumentID != uuid.Nil || library.Title != "" {
		t.Errorf("library conversation = %+v, want no document and no title", library)
	}
	about, err := f.s.CreateConversation(ctx, f.ada.ID, doc)
	if err != nil {
		t.Fatal(err)
	}
	if about.DocumentID != doc || about.DocumentTitle != "Guide" {
		t.Errorf("document conversation = %+v, want document %v titled Guide", about, doc)
	}
	if _, err := f.s.CreateConversation(ctx, f.grace.ID, doc); !errors.Is(err, ErrNotFound) {
		t.Errorf("Grace's conversation about Ada's document: err = %v, want ErrNotFound", err)
	}

	if err := f.s.TouchConversation(ctx, f.ada.ID, library.ID, "first question"); err != nil {
		t.Fatal(err)
	}
	if err := f.s.TouchConversation(ctx, f.ada.ID, library.ID, "second question"); err != nil {
		t.Fatal(err)
	}
	got, err := f.s.GetConversation(ctx, f.ada.ID, library.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "first question" || !got.UpdatedAt.After(library.UpdatedAt) {
		t.Errorf("touched conversation = %+v, want the first question's title and a later updated_at", got)
	}

	page, err := f.s.ListConversations(ctx, f.ada.ID, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 1 || page[0].ID != library.ID {
		t.Fatalf("first page = %+v, want the conversation touched last", page)
	}
	rest, err := f.s.ListConversations(ctx, f.ada.ID, &ConversationCursor{UpdatedAt: page[0].UpdatedAt, ID: page[0].ID}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rest) != 1 || rest[0].ID != about.ID {
		t.Errorf("second page = %+v, want the document conversation", rest)
	}

	if _, err := f.s.GetConversation(ctx, f.grace.ID, library.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Grace reading Ada's conversation: err = %v, want ErrNotFound", err)
	}
	if err := f.s.DeleteConversation(ctx, f.grace.ID, library.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Grace deleting Ada's conversation: err = %v, want ErrNotFound", err)
	}
	if err := f.s.LockConversation(ctx, f.grace.ID, library.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Grace locking Ada's conversation: err = %v, want ErrNotFound", err)
	}
	if list, err := f.s.ListConversations(ctx, f.grace.ID, nil, 10); err != nil || len(list) != 0 {
		t.Errorf("Grace's conversations = %v, %v; want none", list, err)
	}
	if err := f.s.DeleteConversation(ctx, f.ada.ID, library.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.GetConversation(ctx, f.ada.ID, library.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted conversation: err = %v, want ErrNotFound", err)
	}
}

func TestMessages(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	doc := f.document(t, f.ada)
	conv, err := f.s.CreateConversation(ctx, f.ada.ID, uuid.Nil)
	if err != nil {
		t.Fatal(err)
	}

	question := MessageContent{Segments: []Segment{{Text: "How do goroutines communicate?"}}}
	if _, err := f.s.AddMessage(ctx, f.ada.ID, conv.ID, RoleUser, MessageDone, question); err != nil {
		t.Fatal(err)
	}
	answer, err := f.s.AddMessage(ctx, f.ada.ID, conv.ID, RoleAssistant, MessageStreaming, MessageContent{})
	if err != nil {
		t.Fatal(err)
	}
	if running, err := f.s.AnswerRunning(ctx, f.ada.ID, conv.ID); err != nil || !running {
		t.Errorf("AnswerRunning while streaming = %v, %v; want true", running, err)
	}
	if running, err := f.s.AnswerRunning(ctx, f.grace.ID, conv.ID); err != nil || running {
		t.Errorf("AnswerRunning as Grace = %v, %v; want false", running, err)
	}
	if _, err := f.s.AddMessage(ctx, f.grace.ID, conv.ID, RoleUser, MessageDone, question); err == nil {
		t.Error("Grace added a message to Ada's conversation")
	}

	said := MessageContent{
		Segments: []Segment{{Text: "Through channels"}, {Text: ".", Citations: []int{1}}},
		Citations: []Citation{{
			N: 1, DocumentID: doc, BlockIDs: []string{"b1", "b2"}, CitedText: "Share memory by communicating.", Title: "Guide > Channels",
		}},
	}
	usage := MessageUsage{
		Model: "claude-haiku-5-5", InputTokens: 4000, OutputTokens: 200, CacheCreationInputTokens: 600,
		CacheReadInputTokens: 0, CostMicroUSD: 575, FirstTokenMS: 700, LatencyMS: 2100, ToolRounds: 1, InvalidCitations: 2,
	}
	if _, err := f.s.FinishMessage(ctx, f.grace.ID, answer.ID, MessageDone, said, usage); !errors.Is(err, ErrNotFound) {
		t.Errorf("Grace finishing Ada's answer: err = %v, want ErrNotFound", err)
	}
	done, err := f.s.FinishMessage(ctx, f.ada.ID, answer.ID, MessageDone, said, usage)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != MessageDone || done.Usage != usage {
		t.Errorf("finished answer = %+v, want done with %+v", done, usage)
	}
	if _, err := f.s.FinishMessage(ctx, f.ada.ID, answer.ID, MessageStopped, said, usage); !errors.Is(err, ErrNotFound) {
		t.Errorf("finishing an answer twice: err = %v, want ErrNotFound", err)
	}
	if running, err := f.s.AnswerRunning(ctx, f.ada.ID, conv.ID); err != nil || running {
		t.Errorf("AnswerRunning after done = %v, %v; want false", running, err)
	}

	msgs, err := f.s.ListMessages(ctx, f.ada.ID, conv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || msgs[0].Role != RoleUser || msgs[1].Role != RoleAssistant {
		t.Fatalf("messages = %+v, want the question, then the answer", msgs)
	}
	if got := msgs[1].Content; len(got.Citations) != 1 || got.Citations[0].BlockIDs[1] != "b2" || got.Segments[1].Citations[0] != 1 {
		t.Errorf("answer content = %+v, want it as saved", got)
	}
	if msgs, err := f.s.ListMessages(ctx, f.grace.ID, conv.ID); err != nil || len(msgs) != 0 {
		t.Errorf("Grace listing Ada's messages = %v, %v; want none", msgs, err)
	}
}

func TestStaleAnswerReadsAsFailed(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	conv, err := f.s.CreateConversation(ctx, f.ada.ID, uuid.Nil)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := f.s.AddMessage(ctx, f.ada.ID, conv.ID, RoleAssistant, MessageStreaming, MessageContent{})
	if err != nil {
		t.Fatal(err)
	}
	// Its process died long ago.
	if _, err := testDB.Pool.Exec(ctx, "UPDATE messages SET created_at = now() - $2::interval WHERE id = $1", answer.ID, (RunningFor + time.Second).String()); err != nil {
		t.Fatal(err)
	}
	if running, err := f.s.AnswerRunning(ctx, f.ada.ID, conv.ID); err != nil || running {
		t.Errorf("AnswerRunning for a stale answer = %v, %v; want false", running, err)
	}
	msgs, err := f.s.ListMessages(ctx, f.ada.ID, conv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Status != MessageFailed {
		t.Errorf("stale answer = %+v, want it read as failed", msgs)
	}
}

func TestConversationsGoWithTheirDocumentAndUser(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	doc := f.document(t, f.ada)
	about, err := f.s.CreateConversation(ctx, f.ada.ID, doc)
	if err != nil {
		t.Fatal(err)
	}
	library, err := f.s.CreateConversation(ctx, f.ada.ID, uuid.Nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []Conversation{about, library} {
		if _, err := f.s.AddMessage(ctx, f.ada.ID, c.ID, RoleUser, MessageDone, MessageContent{Segments: []Segment{{Text: "q"}}}); err != nil {
			t.Fatal(err)
		}
	}
	count := func(table string) int {
		t.Helper()
		var n int
		if err := testDB.Pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	if _, err := testDB.Pool.Exec(ctx, "DELETE FROM documents WHERE id = $1", doc); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.GetConversation(ctx, f.ada.ID, about.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("conversation about a deleted document: err = %v, want ErrNotFound", err)
	}
	if conversations, messages := count("conversations"), count("messages"); conversations != 1 || messages != 1 {
		t.Errorf("after deleting the document: %d conversations, %d messages; want the library one and its message", conversations, messages)
	}

	if err := f.s.DeleteUser(ctx, f.ada.ID); err != nil {
		t.Fatal(err)
	}
	if conversations, messages := count("conversations"), count("messages"); conversations != 0 || messages != 0 {
		t.Errorf("after deleting the user: %d conversations, %d messages; want none", conversations, messages)
	}
}

func TestChatUsage(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	conv, err := f.s.CreateConversation(ctx, f.ada.ID, uuid.Nil)
	if err != nil {
		t.Fatal(err)
	}
	ask := func(input, cacheWrite, cacheRead int) {
		t.Helper()
		if _, err := f.s.AddMessage(ctx, f.ada.ID, conv.ID, RoleUser, MessageDone, MessageContent{}); err != nil {
			t.Fatal(err)
		}
		a, err := f.s.AddMessage(ctx, f.ada.ID, conv.ID, RoleAssistant, MessageStreaming, MessageContent{})
		if err != nil {
			t.Fatal(err)
		}
		u := MessageUsage{InputTokens: input, CacheCreationInputTokens: cacheWrite, CacheReadInputTokens: cacheRead}
		if _, err := f.s.FinishMessage(ctx, f.ada.ID, a.ID, MessageDone, MessageContent{}, u); err != nil {
			t.Fatal(err)
		}
	}
	ask(1000, 600, 0)
	ask(900, 0, 600)
	// A day and more ago: outside the window.
	if _, err := testDB.Pool.Exec(ctx, "UPDATE messages SET created_at = now() - interval '25 hours' WHERE id = (SELECT id FROM messages WHERE role = 'user' ORDER BY created_at, id LIMIT 1)"); err != nil {
		t.Fatal(err)
	}

	u, err := f.s.ChatUsage(ctx, f.ada.ID, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if u.Questions != 1 || u.InputTokens != 3100 {
		t.Errorf("usage = %+v, want 1 question and 3100 input tokens", u)
	}
	if u.UntilOldestQuestionLeaves < 23*time.Hour || u.UntilOldestAnswerLeaves < 23*time.Hour {
		t.Errorf("usage = %+v, want about a day until the oldest leave", u)
	}
	if g, err := f.s.ChatUsage(ctx, f.grace.ID, 24*time.Hour); err != nil || g.Questions != 0 || g.InputTokens != 0 {
		t.Errorf("Grace's usage = %+v, %v; want none", g, err)
	}
}
