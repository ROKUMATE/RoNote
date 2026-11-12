package search

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/embed"
	"github.com/rokumate/ronote/internal/store"
)

// QueryPrefix is Qwen3-Embedding's instruction for queries; documents are
// embedded without one.
const QueryPrefix = "Instruct: Given a web search query, retrieve relevant passages that answer the query\nQuery: "

// Search limits (docs/adr/0011-retrieval.md).
const (
	MaxQuery     = 500
	MaxLimit     = 50
	DefaultLimit = 20
	// DefaultK is reciprocal rank fusion's constant.
	DefaultK = 60
	// queryTimeout is how long a query's embedding may take before search
	// gives word matches alone.
	queryTimeout = 2 * time.Second
)

// Mode is which halves of a search run. The API always searches Hybrid;
// the eval compares all three.
type Mode int

// Modes.
const (
	Hybrid Mode = iota
	WordsOnly
	MeaningOnly
)

// ErrNoEmbedder means a meaning-only search had no embedder to ask.
var ErrNoEmbedder = errors.New("the embedder is unavailable")

// Request is one search.
type Request struct {
	Owner uuid.UUID
	Query string
	// Document limits the search to one document; uuid.Nil means the
	// owner's whole library.
	Document uuid.UUID
	Limit    int
	Mode     Mode
}

// Segment is part of a snippet: Mark is set on the words the query matched.
type Segment struct {
	Text string
	Mark bool
}

// Hit is a chunk a search found.
type Hit struct {
	DocumentID  uuid.UUID
	Title       string
	BlockID     string
	HeadingPath []string
	Kind        Kind
	Snippet     []Segment
	// Score is the fused score; WordsRank and MeaningRank the chunk's
	// rank in each half, 0 when that half didn't find it; Distance its
	// cosine distance from the query, -1 when it wasn't measured.
	Score                  float64
	WordsRank, MeaningRank int
	Distance               float64
}

// Results are a search's hits, best first. Degraded is set when meaning
// couldn't be searched, so the hits are word matches alone.
type Results struct {
	Hits     []Hit
	Degraded bool
}

// Searcher searches users' chunks.
type Searcher struct {
	store    *store.Store
	embedder embed.Embedder
	logger   *slog.Logger
	// K is reciprocal rank fusion's constant, DefaultK when zero.
	K int
}

// NewSearcher returns a Searcher over st that embeds queries with e.
func NewSearcher(st *store.Store, e embed.Embedder, logger *slog.Logger) *Searcher {
	return &Searcher{store: st, embedder: e, logger: logger}
}

// Search runs r. When the embedder doesn't answer in time, a hybrid search
// gives word matches alone and says so; a meaning-only one fails.
func (s *Searcher) Search(ctx context.Context, r Request) (Results, error) {
	q := store.SearchQuery{
		Owner: r.Owner, Document: r.Document, Text: r.Query, IdentQuery: IdentQuery(r.Query),
		Words: r.Mode != MeaningOnly, Model: s.embedder.Model(), K: s.K, Limit: r.Limit,
	}
	if q.K == 0 {
		q.K = DefaultK
	}
	var res Results
	if r.Mode != WordsOnly {
		vector, err := s.embedQuery(ctx, r.Query)
		switch {
		case err == nil:
			q.Vector = vector
		case r.Mode == MeaningOnly:
			return Results{}, errors.Join(ErrNoEmbedder, err)
		default:
			s.logger.LogAttrs(ctx, slog.LevelWarn, "search by meaning unavailable", slog.String("error", err.Error()))
			res.Degraded = true
		}
	}
	rows, err := s.store.SearchChunks(ctx, q)
	if err != nil {
		return Results{}, err
	}
	res.Hits = make([]Hit, len(rows))
	for i, row := range rows {
		res.Hits[i] = Hit{
			DocumentID: row.DocumentID, Title: row.Title, BlockID: row.BlockID, HeadingPath: row.HeadingPath,
			Kind: Kind(row.Kind), Snippet: segments(row.Snippet), Score: row.Score,
			WordsRank: int(row.WordsRank), MeaningRank: int(row.MeaningRank), Distance: row.Distance,
		}
	}
	return res, nil
}

func (s *Searcher) embedQuery(ctx context.Context, query string) ([]float32, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	vectors, err := s.embedder.Embed(ctx, []string{QueryPrefix + query})
	if err != nil {
		return nil, err
	}
	return vectors[0], nil
}

// The marks ts_headline puts around matched words: private-use characters,
// which no page's text means anything by.
const (
	startMark = ""
	stopMark  = ""
)

// segments splits a marked snippet into its plain and matched parts.
func segments(snippet string) []Segment {
	var out []Segment
	rest := snippet
	for rest != "" {
		before, after, found := strings.Cut(rest, startMark)
		if before != "" {
			out = append(out, Segment{Text: before})
		}
		if !found {
			break
		}
		marked, tail, _ := strings.Cut(after, stopMark)
		if marked != "" {
			out = append(out, Segment{Text: marked, Mark: true})
		}
		rest = tail
	}
	return out
}
