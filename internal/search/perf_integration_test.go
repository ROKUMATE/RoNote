//go:build perf && integration

package search

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/embed"
	"github.com/rokumate/ronote/internal/ir"
	"github.com/rokumate/ronote/internal/store"
)

// perfQueries are what the timed searches ask, by words and meaning.
var perfQueries = []string{
	"how do I run things concurrently", "ServeMux", "error handling in loops",
	"install the package", "for statement", "useEffect cleanup", "slice capacity", "retry with backoff",
}

// perfWords are the words chunks are made of, so word searches find some.
const perfWords = "concurrently goroutine channel loop error install package statement slice capacity retry backoff handler server request response"

// fill gives owner n more chunks in doc, spread over 64 vectors, as fast as
// SQL makes them.
func fill(t *testing.T, owner, doc uuid.UUID, from, n int) {
	t.Helper()
	_, err := testDB.Pool.Exec(t.Context(), `
		WITH words AS (SELECT regexp_split_to_array($5, ' ') AS w),
		vecs AS (
			SELECT v, (SELECT array_agg(sin(v * 7.1 + d * 0.37))::real[] FROM generate_series(1, 1024) d)::halfvec AS embedding
			FROM generate_series(0, 63) v
		)
		INSERT INTO chunks (owner_id, document_id, block_ids, heading_path, kind, text, position, tsv, embedding, model)
		SELECT $1, $2, ARRAY['000000000001'], ARRAY['Section ' || (g % 50)], 'prose', t.text, g,
			to_tsvector('english', t.text), vecs.embedding, $6
		FROM generate_series($3::int, $3::int + $4::int - 1) g
		CROSS JOIN words
		CROSS JOIN LATERAL (SELECT 'Chunk ' || g || ' about ' || w[1 + g % 16] || ' and ' || w[1 + (g * 7) % 16] || ' ' || repeat('text ', 60) AS text) t
		JOIN vecs ON vecs.v = g % 64`,
		owner, doc, from, n, perfWords, embed.FakeModel)
	if err != nil {
		t.Fatal(err)
	}
}

// timeSearches runs every perf query rounds times as owner, in mode,
// returning each search's time.
func timeSearches(t *testing.T, s *Searcher, owner uuid.UUID, mode Mode, rounds int) []time.Duration {
	t.Helper()
	var times []time.Duration
	for range rounds {
		for _, q := range perfQueries {
			start := time.Now()
			if _, err := s.Search(t.Context(), Request{Owner: owner, Query: q, Limit: 20, Mode: mode}); err != nil {
				t.Fatal(err)
			}
			times = append(times, time.Since(start))
		}
	}
	return times
}

func percentile(times []time.Duration, p float64) time.Duration {
	sorted := slices.Clone(times)
	slices.Sort(sorted)
	return sorted[int(math.Ceil(p*float64(len(sorted))))-1]
}

// perfLibrary makes a user with a document, and another user with 200,000
// chunks, under storage for the embedding column.
func perfLibrary(t *testing.T, storage string) (*store.Store, uuid.UUID, uuid.UUID) {
	t.Helper()
	testDB.Reset(t)
	if _, err := testDB.Pool.Exec(t.Context(), "ALTER TABLE chunks ALTER COLUMN embedding SET STORAGE "+storage); err != nil {
		t.Fatal(err)
	}
	st := store.New(testDB.Pool)
	one := &ir.Doc{IRVersion: ir.Version, Title: "Library", Blocks: []ir.Block{{ID: "000000000001", Kind: ir.KindParagraph, Content: []ir.Inline{{Text: "seed"}}}}}
	owner, crowd := newUser(t, st), newUser(t, st)
	doc, crowdDoc := importDoc(t, st, owner, one), importDoc(t, st, crowd, one)
	fill(t, crowd, crowdDoc, 0, 200_000)
	return st, owner, doc
}

func analyze(t *testing.T) {
	t.Helper()
	if _, err := testDB.Pool.Exec(t.Context(), "ANALYZE chunks"); err != nil {
		t.Fatal(err)
	}
}

// TestPerfSearchGrowsLinearly times search over one user's 5k, 10k and 20k
// chunks, with 200,000 of other users' chunks in the table. It checks the
// meaning half, which reads every one of the user's rows: an exact scan
// grows with them, about 2x for each doubling, and anything worse reads
// more than the user's rows. Hybrid times, which add fixed costs such as
// snippets, are logged.
func TestPerfSearchGrowsLinearly(t *testing.T) {
	st, owner, doc := perfLibrary(t, "PLAIN")
	s := NewSearcher(st, embed.Fake{}, discard)
	var best []time.Duration
	have := 0
	for _, size := range []int{5_000, 10_000, 20_000} {
		fill(t, owner, doc, have, size-have)
		have = size
		analyze(t)
		timeSearches(t, s, owner, Hybrid, 1) // warm the cache
		meaning := timeSearches(t, s, owner, MeaningOnly, 5)
		hybrid := timeSearches(t, s, owner, Hybrid, 5)
		best = append(best, slices.Min(meaning))
		t.Logf("%6d chunks: meaning only fastest %v, p50 %v; hybrid p50 %v, p95 %v", size, slices.Min(meaning),
			percentile(meaning, 0.5), percentile(hybrid, 0.5), percentile(hybrid, 0.95))
	}
	for i := 1; i < len(best); i++ {
		if ratio := float64(best[i]) / float64(best[i-1]); ratio > 2.6 {
			t.Errorf("doubling the user's chunks took %.1fx as long (%v, then %v); want about 2x", ratio, best[i-1], best[i])
		}
	}
}

// TestPerfSearchAt25k reports search times over one user's 25,000 chunks,
// with the embedding column stored EXTERNAL (pgvector's default: out of
// line once a row passes 2 kB) and PLAIN (in the row).
func TestPerfSearchAt25k(t *testing.T) {
	for _, storage := range []string{"EXTERNAL", "PLAIN"} {
		t.Run(storage, func(t *testing.T) {
			st, owner, doc := perfLibrary(t, storage)
			fill(t, owner, doc, 0, 25_000)
			analyze(t)
			var rowBytes int
			if err := testDB.Pool.QueryRow(t.Context(), "SELECT avg(pg_column_size(c.*))::int FROM chunks c WHERE owner_id = $1", owner).Scan(&rowBytes); err != nil {
				t.Fatal(err)
			}
			for _, mode := range []struct {
				name string
				m    Mode
			}{{"hybrid", Hybrid}, {"meaning only", MeaningOnly}} {
				s := NewSearcher(st, embed.Fake{}, discard)
				search := func() []time.Duration {
					var times []time.Duration
					for range 10 {
						for _, q := range perfQueries {
							start := time.Now()
							if _, err := s.Search(t.Context(), Request{Owner: owner, Query: q, Limit: 20, Mode: mode.m}); err != nil {
								t.Fatal(err)
							}
							times = append(times, time.Since(start))
						}
					}
					return times
				}
				search()
				times := search()
				t.Logf("%s, %s, 25k chunks (rows of %d bytes in the heap): p50 %v, p95 %v",
					storage, mode.name, rowBytes, percentile(times, 0.5), percentile(times, 0.95))
			}
		})
	}
}
