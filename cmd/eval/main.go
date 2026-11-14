// Command eval measures search against a fixed corpus and set of queries
// (docs/adr/0011-retrieval.md). It loads testdata/retrieval/corpus.txt into
// a throwaway Postgres, indexes it, runs testdata/retrieval/queries.jsonl by
// words alone, by meaning alone and hybrid, and prints recall@5, recall@10,
// MRR@10 and nDCG@10 per category.
//
// By default it replays the vectors recorded for the corpus and queries, so
// no model is needed, and fails when a hybrid metric falls more than 0.02
// below baseline.json, or when meaning no longer beats words on paraphrases
// (as broken vectors would make it).
//
//	go run ./cmd/eval [-record] [-update-baseline] [-review]
//
// -record embeds everything through RONOTE_EMBED_URL and RONOTE_EMBED_MODEL
// (make eval-record) and writes the vectors; -update-baseline writes this
// run's hybrid metrics as the baseline. -review (make eval-review) prints the
// queries marked for review instead of measuring, and writes nothing.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/config"
	"github.com/rokumate/ronote/internal/embed"
	"github.com/rokumate/ronote/internal/ir"
	"github.com/rokumate/ronote/internal/pgtest"
	"github.com/rokumate/ronote/internal/search"
	"github.com/rokumate/ronote/internal/store"
)

const (
	dir = "testdata/retrieval"
	// tolerance is how far a hybrid metric may fall below the baseline.
	tolerance = 0.02
	// paraphraseMargin is how far meaning must beat words on paraphrases'
	// recall@10. It measured 0.20 ahead when recorded; vectors pooled wrong,
	// or queries missing their prefix, fall far below words.
	paraphraseMargin = 0.15
	topK             = 10
)

func main() {
	record := flag.Bool("record", false, "embed the corpus and queries with the configured model and record the vectors")
	updateBaseline := flag.Bool("update-baseline", false, "write this run's hybrid metrics as the baseline")
	reviewOnly := flag.Bool("review", false, "print the queries marked for review with their expected blocks and hybrid's top 5, and write nothing")
	flag.Parse()
	if *reviewOnly && (*record || *updateBaseline) {
		fmt.Fprintln(os.Stderr, "eval: -review writes nothing, so it can't go with -record or -update-baseline")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, options{record: *record, updateBaseline: *updateBaseline, review: *reviewOnly})
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "eval:", err)
		os.Exit(1)
	}
}

// query is one line of queries.jsonl. Expect lists the blocks that answer
// it: for a multidoc query all of them, from each document; for any other,
// any one of them. A none query has no answer in the corpus.
type query struct {
	ID       string   `json:"id"`
	Category string   `json:"category"`
	Query    string   `json:"query"`
	Expect   []string `json:"expect"`
	Review   bool     `json:"review,omitempty"`
}

// metrics are one query's, or a category's mean.
type metrics struct {
	Recall5  float64 `json:"recall@5"`
	Recall10 float64 `json:"recall@10"`
	MRR10    float64 `json:"mrr@10"`
	NDCG10   float64 `json:"ndcg@10"`
}

func (m metrics) values() []float64 { return []float64{m.Recall5, m.Recall10, m.MRR10, m.NDCG10} }

var metricNames = []string{"recall@5", "recall@10", "mrr@10", "ndcg@10"}

type options struct {
	record, updateBaseline, review bool
}

func run(ctx context.Context, opts options) error {
	docs, err := loadCorpus(filepath.Join(dir, "corpus.txt"))
	if err != nil {
		return err
	}
	queries, err := loadQueries(filepath.Join(dir, "queries.jsonl"))
	if err != nil {
		return err
	}
	model := envOr("RONOTE_EMBED_MODEL", config.DefaultEmbedModel)
	vectorsPath := filepath.Join(dir, "vectors-"+strings.NewReplacer(":", "-", "/", "-").Replace(model)+".bin")

	db, err := pgtest.Start(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err := db.Close(); err != nil {
			fmt.Fprintln(os.Stderr, "eval: close database:", err)
		}
	}()
	st := store.New(db.Pool)
	user, err := st.CreateUser(ctx, "eval@ronote.local", "eval")
	if err != nil {
		return err
	}

	var embedder embed.Embedder
	var recorded *embed.Vectors
	if opts.record {
		recorded = embed.NewVectors(model)
		embedder = recorder{Embedder: embed.NewClient(envOr("RONOTE_EMBED_URL", config.DefaultEmbedURL), model), vectors: recorded}
	} else {
		f, err := os.Open(vectorsPath) //nolint:gosec // the recorded vectors' path
		if err != nil {
			return fmt.Errorf("%w; run make eval-record", err)
		}
		v, err := embed.ReadVectors(f)
		_ = f.Close()
		if err != nil {
			return err
		}
		embedder = embed.NewReplay(v)
	}

	discard := slog.New(slog.DiscardHandler)
	indexer := search.NewIndexer(st, embedder, discard)
	chunks := 0
	ids := make([]uuid.UUID, len(docs))
	for i, d := range docs {
		id, err := st.AddDocument(ctx, user.ID, d.doc.Source.URL, d.doc)
		if err != nil {
			return fmt.Errorf("%s: %w", d.path, err)
		}
		r, err := indexer.Index(ctx, user.ID, id)
		if err != nil {
			return fmt.Errorf("%s: %w", d.path, err)
		}
		chunks += r.Chunks
		ids[i] = id
	}
	fmt.Printf("Corpus: %d documents, %d chunks; %d queries; model %s\n\n", len(docs), chunks, len(queries), model)

	if opts.review {
		return review(ctx, st, search.NewSearcher(st, embedder, discard), user.ID, docs, ids, queries)
	}
	if opts.record {
		inputs := make([]string, len(queries))
		for i, q := range queries {
			inputs[i] = search.QueryPrefix + q.Query
		}
		if _, err := embedder.Embed(ctx, inputs); err != nil {
			return err
		}
		if err := writeVectors(vectorsPath, recorded); err != nil {
			return err
		}
		fmt.Printf("Recorded %d vectors in %s\n\n", recorded.Len(), vectorsPath)
		// Evaluate what make eval will replay: the vectors as recorded.
		embedder = embed.NewReplay(recorded)
	}

	searcher := search.NewSearcher(st, embedder, discard)
	results := map[string]map[string]metrics{} // mode → category → mean
	tops := map[string][]topScores{}
	for _, mode := range []struct {
		name string
		m    search.Mode
	}{{"words", search.WordsOnly}, {"meaning", search.MeaningOnly}, {"hybrid", search.Hybrid}} {
		means, scores, err := evaluate(ctx, searcher, user.ID, queries, mode.m)
		if err != nil {
			return fmt.Errorf("%s: %w", mode.name, err)
		}
		results[mode.name] = means
		tops[mode.name] = scores
	}
	printTable(results)
	if err := printK(ctx, st, embedder, user.ID, queries); err != nil {
		return err
	}
	printNoAnswer(tops["hybrid"], tops["meaning"])

	var problems []string
	if easy := tooEasy(results["hybrid"]); easy {
		problems = append(problems, "hybrid recall@10 is at least 0.97 in every category: the set is too easy; add harder queries")
	}
	if p := results["meaning"]["paraphrase"].Recall10 - results["words"]["paraphrase"].Recall10; p < paraphraseMargin {
		problems = append(problems, fmt.Sprintf(
			"meaning beats words on paraphrases' recall@10 by %.2f, want at least %.2f: are the vectors pooled right, and do queries carry their prefix?", p, paraphraseMargin))
	}
	baselinePath := filepath.Join(dir, "baseline.json")
	if opts.updateBaseline {
		rounded := map[string]metrics{}
		for cat, m := range results["hybrid"] {
			r := func(x float64) float64 { return math.Round(x*1e4) / 1e4 }
			rounded[cat] = metrics{r(m.Recall5), r(m.Recall10), r(m.MRR10), r(m.NDCG10)}
		}
		if err := writeJSON(baselinePath, rounded); err != nil {
			return err
		}
		fmt.Printf("\nWrote %s\n", baselinePath)
	} else {
		dropped, err := compareBaseline(baselinePath, results["hybrid"])
		if err != nil {
			problems = append(problems, err.Error())
		}
		problems = append(problems, dropped...)
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "\n"))
	}
	fmt.Println("\neval: ok")
	return nil
}

// document is one extraction of the corpus.
type document struct {
	path string
	doc  *ir.Doc
}

// name is the document's file, or its golden fixture's.
func (d document) name() string {
	if filepath.Base(d.path) == "expected.json" {
		return "golden " + filepath.Base(filepath.Dir(d.path))
	}
	return strings.TrimSuffix(filepath.Base(d.path), ".json")
}

func loadCorpus(manifest string) ([]document, error) {
	f, err := os.Open(manifest) //nolint:gosec // the corpus manifest's path
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var docs []document
	seen := map[string]string{} // source URL → path
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		path := strings.TrimSpace(sc.Text())
		if path == "" || strings.HasPrefix(path, "#") {
			continue
		}
		data, err := os.ReadFile(path) //nolint:gosec // a path the manifest lists
		if err != nil {
			return nil, err
		}
		var doc ir.Doc
		if err := json.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		// Identical blocks share an id, so a page and an excerpt of it
		// would both hold every expected block, and fill each other's top
		// 5 with the same text.
		if other, ok := seen[doc.Source.URL]; ok {
			return nil, fmt.Errorf("%s and %s are both %s: keep one", other, path, doc.Source.URL)
		}
		seen[doc.Source.URL] = path
		docs = append(docs, document{path: path, doc: &doc})
	}
	return docs, sc.Err()
}

func loadQueries(path string) ([]query, error) {
	f, err := os.Open(path) //nolint:gosec // the queries' path
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var qs []query
	dec := json.NewDecoder(f)
	for {
		var q query
		if err := dec.Decode(&q); errors.Is(err, io.EOF) {
			return qs, nil
		} else if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		qs = append(qs, q)
	}
}

// topScores are a query's best fused score and its best cosine similarity.
type topScores struct {
	answerable bool
	fused      float64
	similarity float64
}

// evaluate runs every query in mode and returns each category's mean
// metrics, answerable queries alone, with each query's top scores.
func evaluate(ctx context.Context, s *search.Searcher, owner uuid.UUID, queries []query, mode search.Mode) (map[string]metrics, []topScores, error) {
	sums := map[string]metrics{}
	counts := map[string]int{}
	var tops []topScores
	for _, q := range queries {
		res, err := s.Search(ctx, search.Request{Owner: owner, Query: q.Query, Limit: topK, Mode: mode})
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", q.ID, err)
		}
		top := topScores{answerable: len(q.Expect) > 0}
		if len(res.Hits) > 0 {
			top.fused = res.Hits[0].Score
		}
		for _, h := range res.Hits {
			if h.Distance >= 0 {
				top.similarity = max(top.similarity, 1-h.Distance)
			}
		}
		tops = append(tops, top)
		if len(q.Expect) == 0 {
			continue
		}
		m := score(q, res.Hits)
		for _, cat := range []string{q.Category, "all"} {
			sum := sums[cat]
			sums[cat] = metrics{sum.Recall5 + m.Recall5, sum.Recall10 + m.Recall10, sum.MRR10 + m.MRR10, sum.NDCG10 + m.NDCG10}
			counts[cat]++
		}
	}
	means := map[string]metrics{}
	for cat, sum := range sums {
		n := float64(counts[cat])
		means[cat] = metrics{sum.Recall5 / n, sum.Recall10 / n, sum.MRR10 / n, sum.NDCG10 / n}
	}
	return means, tops, nil
}

// score rates one query's hits. A hit is relevant when its chunk holds an
// expected block. For a multidoc query every expected block counts, so
// recall is the share found and each newly found block gains; for any
// other, one found block answers it.
func score(q query, hits []search.Hit) metrics {
	all := q.Category == "multidoc"
	needed := 1
	if all {
		needed = len(q.Expect)
	}
	found := map[string]bool{}
	var m metrics
	dcg := 0.0
	for i, h := range hits[:min(len(hits), topK)] {
		gain := 0.0
		for _, b := range h.BlockIDs {
			if slices.Contains(q.Expect, b) && !found[b] && (all || len(found) == 0) {
				found[b] = true
				gain = 1
			}
		}
		if gain > 0 {
			dcg += 1 / math.Log2(float64(i+2))
			if m.MRR10 == 0 {
				m.MRR10 = 1 / float64(i+1)
			}
		}
		if i == 4 {
			m.Recall5 = float64(len(found)) / float64(needed)
		}
	}
	m.Recall10 = float64(len(found)) / float64(needed)
	if len(hits) < 5 {
		m.Recall5 = m.Recall10
	}
	ideal := 0.0
	for i := range min(needed, topK) {
		ideal += 1 / math.Log2(float64(i+2))
	}
	m.NDCG10 = dcg / ideal
	return m
}

var categories = []string{"identifier", "error", "paraphrase", "howto", "multidoc", "all"}

func printTable(results map[string]map[string]metrics) {
	fmt.Println("| category | mode | recall@5 | recall@10 | MRR@10 | nDCG@10 |")
	fmt.Println("|---|---|---|---|---|---|")
	for _, cat := range categories {
		for _, mode := range []string{"words", "meaning", "hybrid"} {
			m := results[mode][cat]
			fmt.Printf("| %s | %s | %.3f | %.3f | %.3f | %.3f |\n", cat, mode, m.Recall5, m.Recall10, m.MRR10, m.NDCG10)
		}
	}
}

// printK reports hybrid over all answerable queries for several values of
// reciprocal rank fusion's constant.
func printK(ctx context.Context, st *store.Store, e embed.Embedder, owner uuid.UUID, queries []query) error {
	fmt.Println("\nHybrid, all answerable queries, by RRF constant k:")
	fmt.Println("| k | recall@5 | recall@10 | MRR@10 | nDCG@10 |")
	fmt.Println("|---|---|---|---|---|")
	for _, k := range []int{10, 30, 60} {
		s := search.NewSearcher(st, e, slog.New(slog.DiscardHandler))
		s.K = k
		means, _, err := evaluate(ctx, s, owner, queries, search.Hybrid)
		if err != nil {
			return err
		}
		m := means["all"]
		fmt.Printf("| %d | %.3f | %.3f | %.3f | %.3f |\n", k, m.Recall5, m.Recall10, m.MRR10, m.NDCG10)
	}
	return nil
}

// printNoAnswer compares the top scores of queries the corpus can't answer
// with those it can: what P17 sets its "I don't know" threshold from.
func printNoAnswer(hybrid, meaning []topScores) {
	fmt.Println("\nTop scores, queries with no answer against answerable ones (min / median / max):")
	describe := func(name string, tops []topScores, value func(topScores) float64) {
		var yes, no []float64
		for _, t := range tops {
			if t.answerable {
				yes = append(yes, value(t))
			} else {
				no = append(no, value(t))
			}
		}
		fmt.Printf("- %s: no answer %s; answerable %s\n", name, spread(no), spread(yes))
	}
	describe("hybrid fused score", hybrid, func(t topScores) float64 { return t.fused })
	describe("meaning, cosine similarity", meaning, func(t topScores) float64 { return t.similarity })
}

func spread(xs []float64) string {
	if len(xs) == 0 {
		return "none"
	}
	s := slices.Clone(xs)
	slices.Sort(s)
	return fmt.Sprintf("%.3f / %.3f / %.3f", s[0], s[len(s)/2], s[len(s)-1])
}

func tooEasy(hybrid map[string]metrics) bool {
	for _, cat := range categories[:len(categories)-1] {
		if hybrid[cat].Recall10 < 0.97 {
			return false
		}
	}
	return true
}

// compareBaseline names every hybrid metric more than tolerance below the
// baseline's.
func compareBaseline(path string, hybrid map[string]metrics) ([]string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // the baseline's path
	if err != nil {
		return nil, fmt.Errorf("%w; run go run ./cmd/eval -update-baseline", err)
	}
	var baseline map[string]metrics
	if err := json.Unmarshal(data, &baseline); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	var dropped []string
	for cat, want := range baseline {
		got := hybrid[cat].values()
		for i, w := range want.values() {
			if got[i] < w-tolerance {
				dropped = append(dropped, fmt.Sprintf("hybrid %s %s fell to %.3f from the baseline's %.3f", cat, metricNames[i], got[i], w))
			}
		}
	}
	slices.Sort(dropped)
	return dropped, nil
}

// recorder embeds through an Embedder, keeping every vector it returns.
type recorder struct {
	embed.Embedder
	vectors *embed.Vectors
}

func (r recorder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	vecs, err := r.Embedder.Embed(ctx, texts)
	if err != nil {
		return nil, err
	}
	for i, t := range texts {
		r.vectors.Add(t, vecs[i])
	}
	return vecs, nil
}

func writeVectors(path string, v *embed.Vectors) error {
	f, err := os.Create(path) //nolint:gosec // the recorded vectors' path
	if err != nil {
		return err
	}
	if _, err := v.WriteTo(f); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
