package chat

import (
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/store"
)

func TestCitations(t *testing.T) {
	goDoc, pyDoc := uuid.New(), uuid.New()
	concurrency := Passage{
		ChunkID: uuid.New(), DocumentID: goDoc, Title: "Effective Go > Concurrency",
		BlockIDs: []string{"g1", "g2", "g3"}, Texts: []string{"Share by communicating.", "Channels connect goroutines.", "Use select."},
	}
	loops := Passage{
		ChunkID: uuid.New(), DocumentID: pyDoc, Title: "Control Flow > for",
		BlockIDs: []string{"p1"}, Texts: []string{"for iterates over a sequence."},
	}
	sent := []Passage{concurrency, loops}
	at := func(i, start, end int) Cited {
		return Cited{ResultIndex: i, Source: sent[i].Source(), Start: start, End: end}
	}
	tests := []struct {
		name    string
		cites   []Cited
		numbers []int
		cited   []store.Citation
		invalid int
	}{
		{
			name: "a block range of a passage sent", cites: []Cited{at(0, 1, 3)}, numbers: []int{1},
			cited: []store.Citation{{
				N: 1, DocumentID: goDoc, BlockIDs: []string{"g2", "g3"},
				CitedText: "Channels connect goroutines.\n\nUse select.", Title: "Effective Go > Concurrency",
			}},
		},
		{
			name: "the same blocks again keep their number", cites: []Cited{at(1, 0, 1), at(0, 0, 1), at(1, 0, 1)}, numbers: []int{1, 2, 1},
			cited: []store.Citation{
				{N: 1, DocumentID: pyDoc, BlockIDs: []string{"p1"}, CitedText: "for iterates over a sequence.", Title: "Control Flow > for"},
				{N: 2, DocumentID: goDoc, BlockIDs: []string{"g1"}, CitedText: "Share by communicating.", Title: "Effective Go > Concurrency"},
			},
		},
		{name: "a negative index", cites: []Cited{{ResultIndex: -1, Source: concurrency.Source(), Start: 0, End: 1}}, numbers: []int{0}, invalid: 1},
		{name: "an index past the passages sent", cites: []Cited{{ResultIndex: 2, Source: concurrency.Source(), Start: 0, End: 1}}, numbers: []int{0}, invalid: 1},
		{name: "a chunk not sent", cites: []Cited{{ResultIndex: 0, Source: "ronote:chunk/" + uuid.NewString(), Start: 0, End: 1}}, numbers: []int{0}, invalid: 1},
		{name: "another passage's source at this index", cites: []Cited{{ResultIndex: 0, Source: loops.Source(), Start: 0, End: 1}}, numbers: []int{0}, invalid: 1},
		{name: "a block range past the chunk", cites: []Cited{at(1, 0, 2)}, numbers: []int{0}, invalid: 1},
		{name: "a range starting past the chunk", cites: []Cited{at(0, 3, 4)}, numbers: []int{0}, invalid: 1},
		{name: "an empty range", cites: []Cited{at(0, 1, 1)}, numbers: []int{0}, invalid: 1},
		{name: "a backwards range", cites: []Cited{at(0, 2, 1)}, numbers: []int{0}, invalid: 1},
		{name: "a negative start", cites: []Cited{at(0, -1, 1)}, numbers: []int{0}, invalid: 1},
		{
			name: "valid and invalid together", cites: []Cited{at(0, 9, 10), at(0, 0, 1)}, numbers: []int{0, 1}, invalid: 1,
			cited: []store.Citation{{N: 1, DocumentID: goDoc, BlockIDs: []string{"g1"}, CitedText: "Share by communicating.", Title: "Effective Go > Concurrency"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ct := citer{sent: sent}
			var numbers []int
			for _, c := range tt.cites {
				numbers = append(numbers, ct.cite(c))
			}
			if !slices.Equal(numbers, tt.numbers) {
				t.Errorf("numbers = %v, want %v", numbers, tt.numbers)
			}
			if !slices.EqualFunc(ct.cited, tt.cited, func(a, b store.Citation) bool {
				return a.N == b.N && a.DocumentID == b.DocumentID && slices.Equal(a.BlockIDs, b.BlockIDs) && a.CitedText == b.CitedText && a.Title == b.Title
			}) {
				t.Errorf("cited = %+v, want %+v", ct.cited, tt.cited)
			}
			if ct.invalid != tt.invalid {
				t.Errorf("invalid = %d, want %d", ct.invalid, tt.invalid)
			}
		})
	}
}

func TestCost(t *testing.T) {
	haiku, err := PriceOf("claude-haiku-5-5")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		usage Usage
		want  int64
	}{
		{Usage{}, 0},
		// $0.10 per million input tokens: 10,000 tokens are 1,000 micro-dollars.
		{Usage{InputTokens: 10_000}, 1000},
		{Usage{OutputTokens: 300}, 150},
		{Usage{CacheCreationInputTokens: 800, CacheReadInputTokens: 800}, 108},
		// A part of a micro-dollar rounds up.
		{Usage{InputTokens: 1}, 1},
	}
	for _, tt := range tests {
		if got := haiku.Cost(tt.usage); got != tt.want {
			t.Errorf("Cost(%+v) = %d, want %d", tt.usage, got, tt.want)
		}
	}
	if _, err := PriceOf("claude-unknown"); err == nil {
		t.Error("PriceOf an unknown model succeeded")
	}
}
