package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
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

// Where a run against the real model writes its report, and every answer
// it was given.
const (
	chatReport  = "chat-report.md"
	chatAnswers = "chat-answers.jsonl"
)

// answered is one question's answer as the eval scores it. hit is strict: a
// cited block is an expected block. sectionHit is looser: a cited block sits
// under the same headings as an expected one.
type answered struct {
	q          query
	msg        store.Message
	hit        bool
	sectionHit bool
	refused    bool
	inputAll   int
}

// corpusIndex says under which headings each block of the corpus sits, as a
// breadcrumb (the document's title, then its headings).
type corpusIndex struct {
	// section is a block's breadcrumb, per document.
	section map[uuid.UUID]map[string]string
	// sectionsOf lists the breadcrumbs of a block id. Identical blocks share
	// an id across documents, so it may be more than one.
	sectionsOf map[string][]string
}

// indexCorpus reads where the blocks of the documents ids name sit.
func indexCorpus(ctx context.Context, st *store.Store, owner uuid.UUID, ids []uuid.UUID) (corpusIndex, error) {
	idx := corpusIndex{section: map[uuid.UUID]map[string]string{}, sectionsOf: map[string][]string{}}
	for _, id := range ids {
		state, err := st.GetIndexState(ctx, owner, id)
		if err != nil {
			return corpusIndex{}, err
		}
		idx.section[id] = map[string]string{}
		for _, c := range search.Chunks(state.Title, state.Content, nil) {
			where := breadcrumb(state.Title, c.HeadingPath)
			for _, b := range c.BlockIDs {
				idx.section[id][b] = where
				if !slices.Contains(idx.sectionsOf[b], where) {
					idx.sectionsOf[b] = append(idx.sectionsOf[b], where)
				}
			}
		}
	}
	return idx, nil
}

// chatRun bounds a run of the chat eval: ask only the first limit queries
// (0 asks all), and no further question once the answers have cost budget
// micro-dollars.
type chatRun struct {
	limit  int
	budget int64
}

// askAll asks queries in order, within run, printing each answer with what
// it cost; complete is whether it asked them all.
func askAll(queries []query, idx corpusIndex, run chatRun, ask func(query) (store.Message, error), say func(string)) (all []answered, complete bool, err error) {
	n := len(queries)
	if run.limit > 0 {
		n = min(n, run.limit)
	}
	var spent int64
	for i, q := range queries[:n] {
		if spent >= run.budget {
			say(fmt.Sprintf("  stopped: $%.4f spent, the budget is $%.2f\n", float64(spent)/1e6, float64(run.budget)/1e6))
			return all, false, nil
		}
		msg, err := ask(q)
		if err != nil {
			return all, false, fmt.Errorf("%s: %w", q.ID, err)
		}
		spent += msg.Usage.CostMicroUSD
		all = append(all, scoreAnswer(q, msg, idx))
		say(fmt.Sprintf("  %2d/%d %-4s %-7s %d citations  $%.5f  ($%.4f so far)\n",
			i+1, n, q.ID, msg.Status, len(msg.Content.Citations), float64(msg.Usage.CostMicroUSD)/1e6, float64(spent)/1e6))
	}
	return all, n == len(queries), nil
}

// projection is what asking total questions would cost, at the mean cost
// of those answered, in micro-dollars.
func projection(all []answered, total int) int64 {
	if len(all) == 0 {
		return 0
	}
	var cost int64
	for _, a := range all {
		cost += a.msg.Usage.CostMicroUSD
	}
	return cost * int64(total) / int64(len(all))
}

// chatEval asks the queries through the chat service, within run, a
// conversation each over the whole library, and reports per category how
// its answers cite (docs/adr/0012-chat.md). It searches with the recorded
// vectors, and asks the live embedder for what the model searches for
// itself. It writes the report only for a complete run against Anthropic's
// API: a fake model's, or a part's, would mean nothing.
func chatEval(ctx context.Context, st *store.Store, owner uuid.UUID, replay embed.Embedder, idx corpusIndex, queries []query, run chatRun) error {
	modelURL := strings.TrimSuffix(envOr("RONOTE_EVAL_CHAT_URL", config.DefaultChatURL), "/")
	model := envOr("RONOTE_CHAT_MODEL", config.DefaultChatModel)
	key := os.Getenv("RONOTE_ANTHROPIC_API_KEY")
	anthropicAPI := modelURL == config.DefaultChatURL
	if anthropicAPI && key == "" {
		return fmt.Errorf("asking %s needs RONOTE_ANTHROPIC_API_KEY; RONOTE_EVAL_CHAT_URL can point at the fake model for a dry run", modelURL)
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
	fmt.Printf("Asking up to %d of %d questions of %s at %s, within $%.2f\n",
		cmp.Or(min(run.limit, len(queries)), len(queries)), len(queries), model, modelURL, float64(run.budget)/1e6)
	all, complete, err := askAll(queries, idx, run, func(q query) (store.Message, error) {
		conv, err := svc.Create(ctx, owner, uuid.Nil)
		if err != nil {
			return store.Message{}, err
		}
		a, err := svc.Ask(ctx, owner, conv.ID, q.Query)
		if err != nil {
			return store.Message{}, err
		}
		var msg store.Message
		for u := range a.Updates() {
			if d, ok := u.(chat.DoneUpdate); ok {
				msg = d.Message
			}
		}
		if msg.ID == uuid.Nil {
			return store.Message{}, errors.New("the answer wasn't saved")
		}
		return msg, nil
	}, func(line string) { fmt.Print(line) })
	if err != nil {
		return err
	}
	report := chatTable(all, model, modelURL)
	fmt.Println()
	fmt.Print(report)
	if !complete {
		fmt.Printf("\nAll %d would cost about $%.4f at this rate. Not written: a part of the questions.\n",
			len(queries), float64(projection(all, len(queries)))/1e6)
		return nil
	}
	if !anthropicAPI {
		fmt.Printf("\nNot written: %s is not Anthropic's API.\n", modelURL)
		return nil
	}
	path := filepath.Join(dir, chatReport)
	if err := os.WriteFile(path, []byte(report), 0o644); err != nil { //nolint:gosec // a report to commit
		return err
	}
	answersPath := filepath.Join(dir, chatAnswers)
	if err := writeAnswers(answersPath, all, idx); err != nil {
		return err
	}
	fmt.Printf("\nWrote %s and %s\n", path, answersPath)
	return nil
}

// answerRecord is one line of chat-answers.jsonl: what was asked, what the
// model answered, and what it cited and cost. Status is where the answer
// ended (done, stopped or failed), as the store keeps it; the model's own
// stop reason is not stored.
type answerRecord struct {
	ID             string           `json:"id"`
	Category       string           `json:"category"`
	Query          string           `json:"query"`
	Answer         string           `json:"answer"`
	Status         string           `json:"status"`
	Error          string           `json:"error,omitempty"`
	Citations      []citationRecord `json:"citations"`
	Expect         []string         `json:"expect"`
	ExpectSections []string         `json:"expect_sections"`
	Refused        bool             `json:"refused"`
	StrictHit      bool             `json:"strict_hit"`
	SectionHit     bool             `json:"section_hit"`
	ToolRounds     int              `json:"tool_rounds"`
	Usage          usageRecord      `json:"usage"`
}

type citationRecord struct {
	N         int      `json:"n"`
	Blocks    []string `json:"block_ids"`
	Sections  []string `json:"heading_paths"`
	CitedText string   `json:"cited_text"`
}

type usageRecord struct {
	Model                    string `json:"model"`
	InputTokens              int    `json:"input_tokens"`
	OutputTokens             int    `json:"output_tokens"`
	CacheCreationInputTokens int    `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int    `json:"cache_read_input_tokens"`
	InvalidCitations         int    `json:"invalid_citations"`
	FirstTokenMS             int    `json:"first_token_ms"`
	LatencyMS                int    `json:"latency_ms"`
	CostMicroUSD             int64  `json:"cost_micro_usd"`
}

// recordOf is a's line of chat-answers.jsonl. The answer is its segments,
// each followed by the numbers of its citations as [n].
func recordOf(a answered, idx corpusIndex) answerRecord {
	var text strings.Builder
	for _, s := range a.msg.Content.Segments {
		text.WriteString(s.Text)
		for _, n := range s.Citations {
			fmt.Fprintf(&text, "[%d]", n)
		}
	}
	u := a.msg.Usage
	r := answerRecord{
		ID: a.q.ID, Category: a.q.Category, Query: a.q.Query, Answer: text.String(),
		Status: string(a.msg.Status), Error: a.msg.Content.Error,
		Citations: []citationRecord{}, Expect: a.q.Expect, ExpectSections: expectedSections(a.q, idx),
		Refused: a.refused, StrictHit: a.hit, SectionHit: a.sectionHit, ToolRounds: u.ToolRounds,
		Usage: usageRecord{
			Model: u.Model, InputTokens: u.InputTokens, OutputTokens: u.OutputTokens,
			CacheCreationInputTokens: u.CacheCreationInputTokens, CacheReadInputTokens: u.CacheReadInputTokens,
			InvalidCitations: u.InvalidCitations, FirstTokenMS: u.FirstTokenMS, LatencyMS: u.LatencyMS, CostMicroUSD: u.CostMicroUSD,
		},
	}
	for _, c := range a.msg.Content.Citations {
		r.Citations = append(r.Citations, citationRecord{
			N: c.N, Blocks: c.BlockIDs, Sections: citedSections(c, idx), CitedText: c.CitedText,
		})
	}
	return r
}

// writeAnswers writes every answer of all to path, a line each.
func writeAnswers(path string, all []answered, idx corpusIndex) error {
	f, err := os.Create(path) //nolint:gosec // an answers file to commit
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	for _, a := range all {
		if err := enc.Encode(recordOf(a, idx)); err != nil {
			_ = f.Close()
			return err
		}
	}
	return f.Close()
}

// expectedSections are the breadcrumbs of q's expected blocks.
func expectedSections(q query, idx corpusIndex) []string {
	var out []string
	for _, b := range q.Expect {
		for _, where := range idx.sectionsOf[b] {
			if !slices.Contains(out, where) {
				out = append(out, where)
			}
		}
	}
	return out
}

// citedSections are the breadcrumbs of the blocks c cites.
func citedSections(c store.Citation, idx corpusIndex) []string {
	var out []string
	for _, b := range c.BlockIDs {
		if where, ok := idx.section[c.DocumentID][b]; ok && !slices.Contains(out, where) {
			out = append(out, where)
		}
	}
	return out
}

func scoreAnswer(q query, msg store.Message, idx corpusIndex) answered {
	a := answered{q: q, msg: msg, refused: msg.Status == store.MessageDone && len(msg.Content.Citations) == 0}
	wanted := expectedSections(q, idx)
	for _, c := range msg.Content.Citations {
		if slices.ContainsFunc(c.BlockIDs, func(b string) bool { return slices.Contains(q.Expect, b) }) {
			a.hit = true
		}
		if slices.ContainsFunc(citedSections(c, idx), func(where string) bool { return slices.Contains(wanted, where) }) {
			a.sectionHit = true
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
	b.WriteString("| category | questions | citations valid | section hit rate | strict hit rate | false refusals | correct refusals | first token p50 / p95 | total p50 / p95 | mean cost |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|\n")
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
	fmt.Fprintf(&b, "\nBoth hit rates count answerable questions. **Section** (the headline): an answer cites a block under the same headings, in the same document, as an expected block. **Strict**: an answer cites an expected block itself. The labels name one block per answer where the model may cite its neighbours, so strict undercounts. A refusal is an answer with no valid citation, shown as \"Not found in your library\". Every answer, with what it cited, is in `chat-answers.jsonl`. ")
	fmt.Fprintf(&b, "In all: %d tool rounds, %d citations dropped as invalid, %d answers not done; %d input tokens, %.0f%% read from cache; the largest question read %d input tokens over all its calls; total cost $%.4f.\n",
		rounds, invalid, failed, input, percent(cached, input), largest, float64(cost)/1e6)
	return b.String()
}

func chatRow(cat string, rows []answered) string {
	var valid, invalid, answerable, hits, sectionHits, falseRefusals, none, correct int
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
			if a.sectionHit {
				sectionHits++
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
	return fmt.Sprintf("| %s | %d | %s | %s | %s | %s | %s | %.1f s / %.1f s | %.1f s / %.1f s | $%.5f |\n",
		cat, len(rows), validity, share(sectionHits, answerable), share(hits, answerable), share(falseRefusals, answerable), share(correct, none),
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
