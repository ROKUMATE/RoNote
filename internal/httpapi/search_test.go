package httpapi

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/httpapi/gen"
	"github.com/rokumate/ronote/internal/search"
)

// fakeSearcher answers every search with its results, recording requests.
type fakeSearcher struct {
	results  search.Results
	requests []search.Request
}

func (f *fakeSearcher) Search(_ context.Context, r search.Request) (search.Results, error) {
	f.requests = append(f.requests, r)
	return f.results, nil
}

var aHit = search.Hit{
	DocumentID: uuid.MustParse("0192f1c4-0000-7000-8000-000000000010"), Title: "Effective Go", BlockID: "b1",
	HeadingPath: []string{"Concurrency", "Goroutines"}, Kind: search.KindProse,
	Snippet: []search.Segment{{Text: "They're called "}, {Text: "goroutines", Mark: true}},
	Score:   0.03, WordsRank: 1, MeaningRank: 0, Distance: -1,
}

func TestSearch(t *testing.T) {
	t.Parallel()
	doc := uuid.MustParse("0192f1c4-0000-7000-8000-000000000011")
	tests := []struct {
		name       string
		query      string
		scores     bool
		want       int
		wantReq    search.Request
		wantScores bool
	}{
		{name: "the whole library", query: "q=goroutines", want: http.StatusOK, wantReq: search.Request{Owner: testUser, Query: "goroutines", Limit: 20}},
		{name: "one document, a limit", query: "q=goroutines&limit=5&document_id=" + doc.String(), want: http.StatusOK, wantReq: search.Request{Owner: testUser, Query: "goroutines", Limit: 5, Document: doc}},
		{name: "scores in dev and test", query: "q=goroutines", scores: true, want: http.StatusOK, wantReq: search.Request{Owner: testUser, Query: "goroutines", Limit: 20}, wantScores: true},
		{name: "no query", query: "q=%20%20", want: http.StatusBadRequest},
		{name: "a query too long", query: "q=" + strings.Repeat("a", 501), want: http.StatusBadRequest},
		{name: "a limit of 0", query: "q=x&limit=0", want: http.StatusBadRequest},
		{name: "a limit of 51", query: "q=x&limit=51", want: http.StatusBadRequest},
		{name: "a document that isn't an id", query: "q=x&document_id=nope", want: http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := &fakeSearcher{results: search.Results{Hits: []search.Hit{aHit}, Degraded: true}}
			h := signedIn(Options{Logger: discardLogger, DB: dbUp, Backend: &fakeBackend{}, Searcher: s, ShowScores: tt.scores})

			rec := apiRequest(t, h, http.MethodGet, "/api/v1/search?"+tt.query, "")

			if rec.Code != tt.want {
				t.Fatalf("status = %d %s, want %d", rec.Code, rec.Body, tt.want)
			}
			if tt.want != http.StatusOK {
				if len(s.requests) != 0 {
					t.Errorf("searched %+v for a bad request", s.requests)
				}
				return
			}
			if len(s.requests) != 1 || s.requests[0] != tt.wantReq {
				t.Errorf("searched %+v, want %+v", s.requests, tt.wantReq)
			}
			got := decodeJSON[gen.SearchResults](t, rec)
			if !got.Degraded || len(got.Results) != 1 {
				t.Fatalf("results = %+v, want one, degraded", got)
			}
			r := got.Results[0]
			if r.DocumentTitle != "Effective Go" || r.BlockID != "b1" || len(r.HeadingPath) != 2 || r.Kind != "prose" ||
				len(r.Snippet) != 2 || !r.Snippet[1].Mark || r.Snippet[1].Text != "goroutines" {
				t.Errorf("result = %+v", r)
			}
			if (r.Score != nil) != tt.wantScores {
				t.Errorf("score = %+v, want shown = %v", r.Score, tt.wantScores)
			}
			if r.Score != nil && (r.Score.WordsRank == nil || *r.Score.WordsRank != 1 || r.Score.MeaningRank != nil || r.Score.Distance != nil) {
				t.Errorf("score = %+v, want words rank 1 alone", r.Score)
			}
		})
	}
}

func TestSearchNeedsASession(t *testing.T) {
	t.Parallel()
	h := New(Options{Logger: discardLogger, DB: dbUp, Backend: &fakeBackend{}, Searcher: &fakeSearcher{}, Accounts: newFakeAccounts()})
	rec := record(h, newRequest(t, http.MethodGet, "/api/v1/search?q="+url.QueryEscape("x")))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}
