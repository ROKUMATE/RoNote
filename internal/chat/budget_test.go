package chat

import (
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"
)

func TestHistoryDropsTheOldestFirst(t *testing.T) {
	t.Parallel()
	turn := func(n int) exchange {
		return exchange{question: strings.Repeat("q", 300) + strconv.Itoa(n), answer: strings.Repeat("a", 900)}
	}
	exchanges := []exchange{turn(0), turn(1), turn(2), turn(3)}
	// Each costs 101 + 300 + the overhead.
	each := tokens(exchanges[0].question) + tokens(exchanges[0].answer) + callOverhead
	tests := []struct {
		room int
		want []int
	}{
		{room: 4 * each, want: []int{0, 1, 2, 3}},
		{room: 4*each - 1, want: []int{1, 2, 3}},
		{room: 2 * each, want: []int{2, 3}},
		{room: each - 1, want: nil},
		{room: -5, want: nil},
	}
	for _, tt := range tests {
		got := history(exchanges, tt.room)
		var kept []int
		for i := 0; i < len(got); i += 2 {
			if got[i].Role != User || got[i+1].Role != Assistant {
				t.Fatalf("room %d: messages %+v don't alternate question and answer", tt.room, got)
			}
			q := got[i].Blocks[0].(Text).Text
			kept = append(kept, int(q[len(q)-1]-'0'))
		}
		if !slices.Equal(kept, tt.want) {
			t.Errorf("room %d kept exchanges %v, want %v", tt.room, kept, tt.want)
		}
	}
}

func TestFitCutsAPassageToItsShare(t *testing.T) {
	t.Parallel()
	small := Passage{BlockIDs: []string{"a", "b"}, Texts: []string{"one", "two"}}
	if got := fit(small); !slices.Equal(got.Texts, small.Texts) || !slices.Equal(got.BlockIDs, small.BlockIDs) {
		t.Errorf("fit(small) = %+v, want it unchanged", got)
	}
	half := strings.Repeat("x", maxPassageChars/2)
	big := Passage{BlockIDs: []string{"a", "b", "c", "d"}, Texts: []string{half, half, "after", "more"}}
	got := fit(big)
	if !slices.Equal(got.BlockIDs, []string{"a", "b"}) {
		t.Errorf("a passage full at a block's end keeps blocks %v, want a and b", got.BlockIDs)
	}
	code := Passage{BlockIDs: []string{"p", "c", "q"}, Texts: []string{"Run:", strings.Repeat("y", 3*maxPassageChars), "Done."}}
	got = fit(code)
	if !slices.Equal(got.BlockIDs, []string{"p", "c"}) || len(got.Texts) != 2 || !strings.HasSuffix(got.Texts[1], "…") {
		t.Errorf("an oversized block = %v %d texts, want it cut and the rest dropped", got.BlockIDs, len(got.Texts))
	}
	if n := utf8.RuneCountInString(strings.Join(got.Texts, "")); n > maxPassageChars+1 {
		t.Errorf("cut passage holds %d characters, want at most %d and the ellipsis", n, maxPassageChars)
	}
	if code.Texts[1] != strings.Repeat("y", 3*maxPassageChars) {
		t.Error("fit changed the passage it was given")
	}
}

// TestTheLargestTurnFitsTheCap builds the largest first call a turn can
// make, and the largest each tool round can add, and checks the cap holds:
// history gives way to everything else.
func TestTheLargestTurnFitsTheCap(t *testing.T) {
	t.Parallel()
	largest := func() []Passage {
		var ps []Passage
		for range passagesPerSearch {
			ps = append(ps, fit(Passage{
				ChunkID: uuid.New(), Title: strings.Repeat("T", 120), BlockIDs: []string{"a"}, Texts: []string{strings.Repeat("z", 5*maxPassageChars)},
			}))
		}
		return ps
	}
	question := strings.Repeat("w", MaxQuestion)
	passages := largest()
	var earlier []exchange
	for range 50 {
		earlier = append(earlier, exchange{question: strings.Repeat("q", MaxQuestion), answer: strings.Repeat("a", 4*maxTokens*charsPerToken)})
	}
	asked := []Block{}
	for _, p := range passages {
		asked = append(asked, p.result())
	}
	req := Request{System: systemPrompt, Messages: append(history(earlier, historyRoom(question, passages)), Message{Role: User, Blocks: append(asked, Text{Text: question})})}
	n := firstCallTokens(req)
	// Each round sends back at most maxTokens and adds a search's results.
	for range maxToolRounds {
		n += maxTokens
		result := ToolResult{ToolUseID: "toolu_1"}
		for _, p := range largest() {
			result.Results = append(result.Results, p.result())
		}
		n += blockTokens(result)
	}
	if n > inputCap {
		t.Errorf("the largest turn's last call takes about %d tokens, over the cap of %d", n, inputCap)
	}
}

// TestATypicalTurnKeepsSomeHistory checks the room left for earlier
// exchanges is worth having: a few, beside chunks of a usual size.
func TestATypicalTurnKeepsSomeHistory(t *testing.T) {
	t.Parallel()
	var passages []Passage
	for range passagesPerSearch {
		passages = append(passages, Passage{ChunkID: uuid.New(), Title: "Effective Go > Concurrency > Channels", BlockIDs: []string{"a"}, Texts: []string{strings.Repeat("z", 1400)}})
	}
	e := exchange{question: "How do goroutines communicate?", answer: strings.Repeat("a", 1500)}
	earlier := []exchange{e, e, e, e}
	if got := history(earlier, historyRoom("And what does select do?", passages)); len(got) < 2*3 {
		t.Errorf("a typical turn keeps %d earlier exchanges, want at least 3", len(got)/2)
	}
}
