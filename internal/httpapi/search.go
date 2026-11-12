package httpapi

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/rokumate/ronote/internal/httpapi/gen"
	"github.com/rokumate/ronote/internal/search"
)

// Searcher searches a user's library. *search.Searcher implements it; it is
// an interface so handler tests can run without a database.
type Searcher interface {
	Search(ctx context.Context, r search.Request) (search.Results, error)
}

func (s apiServer) Search(ctx context.Context, req gen.SearchRequestObject) (gen.SearchResponseObject, error) {
	bad := func(message string) (gen.SearchResponseObject, error) {
		return gen.Search400JSONResponse{InvalidParameterJSONResponse: gen.InvalidParameterJSONResponse(apiError(ctx, gen.ErrorCodeInvalidParameter, message))}, nil
	}
	q := strings.TrimSpace(req.Params.Q)
	if q == "" || utf8.RuneCountInString(q) > search.MaxQuery {
		return bad("q must be 1 to 500 characters")
	}
	r := search.Request{Owner: userFrom(ctx), Query: q, Limit: search.DefaultLimit}
	if req.Params.Limit != nil {
		if *req.Params.Limit < 1 || *req.Params.Limit > search.MaxLimit {
			return bad("limit must be 1 to 50")
		}
		r.Limit = *req.Params.Limit
	}
	if req.Params.DocumentID != nil {
		r.Document = *req.Params.DocumentID
	}
	res, err := s.searcher.Search(ctx, r)
	if err != nil {
		return nil, err
	}
	out := gen.Search200JSONResponse{Degraded: res.Degraded, Results: make([]gen.SearchResult, len(res.Hits))}
	for i, h := range res.Hits {
		snippet := make([]gen.SnippetPart, len(h.Snippet))
		for j, seg := range h.Snippet {
			snippet[j] = gen.SnippetPart{Text: seg.Text, Mark: seg.Mark}
		}
		result := gen.SearchResult{
			DocumentID: h.DocumentID, DocumentTitle: h.Title, BlockID: h.BlockID, HeadingPath: h.HeadingPath,
			Kind: gen.SearchResultKind(h.Kind), Snippet: snippet,
		}
		if result.HeadingPath == nil {
			result.HeadingPath = []string{}
		}
		if s.showScores {
			result.Score = scoreJSON(h)
		}
		out.Results[i] = result
	}
	return out, nil
}

// scoreJSON is a hit's score, its ranks and distance only when measured.
func scoreJSON(h search.Hit) *gen.SearchScore {
	score := &gen.SearchScore{Fused: float32(h.Score)}
	if h.WordsRank > 0 {
		score.WordsRank = &h.WordsRank
	}
	if h.MeaningRank > 0 {
		score.MeaningRank = &h.MeaningRank
	}
	if h.Distance >= 0 {
		d := float32(h.Distance)
		score.Distance = &d
	}
	return score
}
