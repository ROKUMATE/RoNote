package main

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/chat"
	"github.com/rokumate/ronote/internal/chat/anthropic"
	"github.com/rokumate/ronote/internal/config"
	"github.com/rokumate/ronote/internal/embed"
	"github.com/rokumate/ronote/internal/search"
	"github.com/rokumate/ronote/internal/store"
)

// chatReport is where a run against the real model writes its report.
const chatReport = "chat-report.md"

// answered is one question's answer as the eval scores it.
type answered struct {
	q        query
	msg      store.Message
	hit      bool
	refused  bool
	inputAll int
}

// chatEval asks every query through the chat service, a conversation each
// over the whole library, and reports per category how its answers cite
// (docs/adr/0012-chat.md). It searches with the recorded vectors, and asks
// the live embedder for what the model searches for itself. It writes the
// report only from Anthropic's API: a fake model's would mean nothing.
func chatEval(ctx context.Context, st *store.Store, owner uuid.UUID, replay embed.Embedder, queries []query) error {
	modelURL := strings.TrimSuffix(envOr("RONOTE_EVAL_CHAT_URL", config.DefaultChatURL), "/")
	model := envOr("RONOTE_CHAT_MODEL", config.DefaultChatModel)
	key := os.Getenv("ANTHROPIC_API_KEY")
	anthropicAPI := modelURL == config.DefaultChatURL
	if anthropicAPI && key == "" {
		return fmt.Errorf("asking %s needs ANTHROPIC_API_KEY; RONOTE_EVAL_CHAT_URL can point at the fake model for a dry run", modelURL)
	}
	if u, err := url.Parse(modelURL); err != nil || u.Host == "" {
		return fmt.Errorf("RONOTE_EVAL_CHAT_URL: %q is not a URL", modelURL)
	}
	discard := slog.New(slog.DiscardHandler)
	live := embed.NewClient(envOr("RONOTE_EMBED_URL", config.DefaultEmbedURL), replay.Model())
	svc, err := chat.NewService(chat.Options{
		Store: st, Searcher: search.NewSearcher(st, replayOrLive{replay: replay, live: live}, discard),
		Model: anthropic.New(modelURL, key, model), ModelName: model, Enabled: true,
		Limits: chat.Limits{MessagesPerDay: math.MaxInt32, TokensPerDay: math.MaxInt32}, Logger: discard,
	})
	if err != nil {
		return err
	}
	fmt.Printf("Asking %d questions of %s at %s\n", len(queries), model, modelURL)
	var all []answered
	for i, q := range queries {
		conv, err := svc.Create(ctx, owner, uuid.Nil)
		if err != nil {
			return err
		}
		a, err := svc.Ask(ctx, owner, conv.ID, q.Query)
		if err != nil {
			return fmt.Errorf("%s: %w", q.ID, err)
		}
		var msg store.Message
		for u := range a.Updates() {
			if d, ok := u.(chat.DoneUpdate); ok {
				msg = d.Message
			}
		}
		if msg.ID == uuid.Nil {
			return fmt.Errorf("%s: the answer wasn't saved", q.ID)
		}
		all = append(all, scoreAnswer(q, msg))
		fmt.Printf("  %2d/%d %-4s %-7s %d citations\n", i+1, len(queries), q.ID, msg.Status, len(msg.Content.Citations))
	}
	report := chatTable(all, model, modelURL)
	fmt.Println()
	fmt.Print(report)
	if !anthropicAPI {
		fmt.Printf("\nNot written: %s is not Anthropic's API.\n", modelURL)
		return nil
	}
	path := filepath.Join(dir, chatReport)
	if err := os.WriteFile(path, []byte(report), 0o644); err != nil { //nolint:gosec // a report to commit
		return err
	}
	fmt.Printf("\nWrote %s\n", path)
	return nil
}

func scoreAnswer(q query, msg store.Message) answered {
	a := answered{q: q, msg: msg, refused: msg.Status == store.MessageDone && len(msg.Content.Citations) == 0}
	for _, c := range msg.Content.Citations {
		if slices.ContainsFunc(c.BlockIDs, func(b string) bool { return slices.Contains(q.Expect, b) }) {
			a.hit = true
		}
	}
	u := msg.Usage
	a.inputAll = u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
	return a
}

// chatTable is the report: a row per category and one for all.
func chatTable(all []answered, model, modelURL string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Chat eval\n\n%s, %d questions of `queries.jsonl` asked of %s at %s, a conversation each over the whole corpus (`make eval-chat`, docs/adr/0012-chat.md).\n\n",
		time.Now().UTC().Format("2006-01-02"), len(all), model, modelURL)
	b.WriteString("| category | questions | citations valid | hit rate | false refusals | correct refusals | first token p50 / p95 | total p50 / p95 | mean cost |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|\n")
	var categories []string
	for _, a := range all {
		if !slices.Contains(categories, a.q.Category) {
			categories = append(categories, a.q.Category)
		}
	}
	for _, cat := range append(categories, "all") {
		var rows []answered
		for _, a := range all {
			if cat == "all" || a.q.Category == cat {
				rows = append(rows, a)
			}
		}
		b.WriteString(chatRow(cat, rows))
	}
	var rounds, invalid, failed, input, cached, largest int
	var cost int64
	for _, a := range all {
		u := a.msg.Usage
		rounds += u.ToolRounds
		invalid += u.InvalidCitations
		input += a.inputAll
		cached += u.CacheReadInputTokens
		largest = max(largest, a.inputAll)
		cost += u.CostMicroUSD
		if a.msg.Status != store.MessageDone {
			failed++
		}
	}
	fmt.Fprintf(&b, "\nHit rate counts answerable questions with a cited block among those expected; a refusal is an answer with no valid citation, shown as \"Not found in your library\". ")
	fmt.Fprintf(&b, "In all: %d tool rounds, %d citations dropped as invalid, %d answers not done; %d input tokens, %.0f%% read from cache; the largest question read %d input tokens over all its calls; total cost $%.4f.\n",
		rounds, invalid, failed, input, percent(cached, input), largest, float64(cost)/1e6)
	return b.String()
}

func chatRow(cat string, rows []answered) string {
	var valid, invalid, answerable, hits, falseRefusals, none, correct int
	var first, total []float64
	var cost int64
	for _, a := range rows {
		valid += len(a.msg.Content.Citations)
		invalid += a.msg.Usage.InvalidCitations
		if a.q.Category == "none" {
			none++
			if a.refused {
				correct++
			}
		} else {
			answerable++
			if a.hit {
				hits++
			}
			if a.refused {
				falseRefusals++
			}
		}
		first = append(first, float64(a.msg.Usage.FirstTokenMS)/1000)
		total = append(total, float64(a.msg.Usage.LatencyMS)/1000)
		cost += a.msg.Usage.CostMicroUSD
	}
	validity := "–"
	if valid+invalid > 0 {
		validity = fmt.Sprintf("%.0f%% (%d/%d)", percent(valid, valid+invalid), valid, valid+invalid)
	}
	return fmt.Sprintf("| %s | %d | %s | %s | %s | %s | %.1f s / %.1f s | %.1f s / %.1f s | $%.5f |\n",
		cat, len(rows), validity, share(hits, answerable), share(falseRefusals, answerable), share(correct, none),
		quantile(first, 0.5), quantile(first, 0.95), quantile(total, 0.5), quantile(total, 0.95),
		float64(cost)/1e6/float64(max(len(rows), 1)))
}

func percent(n, of int) float64 { return 100 * float64(n) / float64(max(of, 1)) }

// share is n of of as a fraction, or "–" when there are none to count.
func share(n, of int) string {
	if of == 0 {
		return "–"
	}
	return fmt.Sprintf("%.2f", float64(n)/float64(of))
}

// quantile is the q-th quantile of xs, the nearest rank.
func quantile(xs []float64, q float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := slices.Clone(xs)
	slices.Sort(s)
	return s[min(len(s)-1, max(0, int(math.Ceil(q*float64(len(s))))-1))]
}

// replayOrLive answers from the recorded vectors, and asks the live
// embedder for a text it has none for: a query the model wrote itself.
type replayOrLive struct {
	replay, live embed.Embedder
}

func (r replayOrLive) Model() string { return r.replay.Model() }

func (r replayOrLive) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if v, err := r.replay.Embed(ctx, texts); err == nil {
		return v, nil
	}
	return r.live.Embed(ctx, texts)
}
