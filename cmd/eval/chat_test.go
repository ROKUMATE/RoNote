package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/store"
)

// costing answers every question at cost micro-dollars.
func costing(cost int64, asked *int) func(query) (store.Message, error) {
	return func(query) (store.Message, error) {
		*asked++
		return store.Message{ID: uuid.New(), Status: store.MessageDone, Usage: store.MessageUsage{CostMicroUSD: cost}}, nil
	}
}

func queriesN(n int) []query {
	qs := make([]query, n)
	for i := range qs {
		qs[i] = query{ID: fmt.Sprintf("q%d", i+1), Category: "howto"}
	}
	return qs
}

func TestAskAll(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		limit     int
		budget    int64
		cost      int64
		wantAsked int
		wantDone  bool
	}{
		{name: "all, under budget", limit: 0, budget: 1_500_000, cost: 3_000, wantAsked: 10, wantDone: true},
		{name: "a limit asks the first N", limit: 4, budget: 1_500_000, cost: 3_000, wantAsked: 4},
		{name: "the budget stops before the next question", limit: 0, budget: 10_000, cost: 3_000, wantAsked: 4},
		{name: "a limit past the end is all", limit: 99, budget: 1_500_000, cost: 1, wantAsked: 10, wantDone: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			asked := 0
			all, complete, err := askAll(queriesN(10), corpusIndex{}, chatRun{limit: tt.limit, budget: tt.budget}, costing(tt.cost, &asked), func(string) {})
			if err != nil {
				t.Fatal(err)
			}
			if asked != tt.wantAsked || len(all) != tt.wantAsked || complete != tt.wantDone {
				t.Errorf("asked %d, kept %d, complete %v; want %d, %d, %v", asked, len(all), complete, tt.wantAsked, tt.wantAsked, tt.wantDone)
			}
		})
	}
}

func TestAskAllStopsOnAnError(t *testing.T) {
	t.Parallel()
	failing := func(query) (store.Message, error) { return store.Message{}, errors.New("model down") }
	if _, _, err := askAll(queriesN(3), corpusIndex{}, chatRun{budget: 1_000_000}, failing, func(string) {}); err == nil {
		t.Error("askAll went on after an error")
	}
}

func TestProjection(t *testing.T) {
	t.Parallel()
	asked := 0
	all, _, err := askAll(queriesN(10), corpusIndex{}, chatRun{limit: 10, budget: 1_500_000}, costing(2_500, &asked), func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if got := projection(all, 80); got != 200_000 {
		t.Errorf("projection = %d micro-dollars, want 80 × 2,500 = 200,000", got)
	}
	if got := projection(nil, 80); got != 0 {
		t.Errorf("projection of nothing = %d, want 0", got)
	}
}

// twoSections is a corpus of one document with two sections: a holds blocks
// a1 and a2, b holds b1. Another document repeats b1 under its own headings.
func twoSections() (corpusIndex, uuid.UUID, uuid.UUID) {
	first, second := uuid.New(), uuid.New()
	return corpusIndex{
		section: map[uuid.UUID]map[string]string{
			first:  {"a1": "Doc > A", "a2": "Doc > A", "b1": "Doc > B"},
			second: {"b1": "Other > B"},
		},
		sectionsOf: map[string][]string{"a1": {"Doc > A"}, "a2": {"Doc > A"}, "b1": {"Doc > B", "Other > B"}},
	}, first, second
}

func citing(doc uuid.UUID, blocks ...string) store.Message {
	return store.Message{Status: store.MessageDone, Content: store.MessageContent{
		Segments:  []store.Segment{{Text: "An answer.", Citations: []int{1}}},
		Citations: []store.Citation{{N: 1, DocumentID: doc, BlockIDs: blocks, CitedText: "text"}},
	}}
}

func TestScoreAnswer(t *testing.T) {
	t.Parallel()
	idx, first, second := twoSections()
	tests := []struct {
		name        string
		msg         store.Message
		wantStrict  bool
		wantSection bool
		wantRefused bool
	}{
		{name: "the expected block", msg: citing(first, "a1"), wantStrict: true, wantSection: true},
		{name: "its neighbour in the section", msg: citing(first, "a2"), wantSection: true},
		{name: "another section", msg: citing(first, "b1")},
		{name: "a section of the same name in another document", msg: citing(second, "b1")},
		{name: "no citation is a refusal", msg: store.Message{Status: store.MessageDone}, wantRefused: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := scoreAnswer(query{ID: "q", Category: "howto", Expect: []string{"a1"}}, tt.msg, idx)
			if a.hit != tt.wantStrict || a.sectionHit != tt.wantSection || a.refused != tt.wantRefused {
				t.Errorf("strict %v, section %v, refused %v; want %v, %v, %v", a.hit, a.sectionHit, a.refused, tt.wantStrict, tt.wantSection, tt.wantRefused)
			}
		})
	}
}

func TestSectionHitSharesAnIdAcrossDocuments(t *testing.T) {
	t.Parallel()
	idx, _, second := twoSections()
	a := scoreAnswer(query{ID: "q", Category: "howto", Expect: []string{"b1"}}, citing(second, "b1"), idx)
	if !a.sectionHit {
		t.Error("b1 is expected under both documents' headings; citing it in the second is a section hit")
	}
}

func TestWriteAnswers(t *testing.T) {
	t.Parallel()
	idx, first, _ := twoSections()
	q := query{ID: "h01", Category: "howto", Query: "how?", Expect: []string{"a1"}}
	msg := citing(first, "a2")
	msg.Usage = store.MessageUsage{Model: "m", ToolRounds: 2, CostMicroUSD: 1500}
	none := scoreAnswer(query{ID: "n01", Category: "none", Query: "what?"}, store.Message{Status: store.MessageDone}, idx)

	path := filepath.Join(t.TempDir(), "answers.jsonl")
	if err := writeAnswers(path, []answered{scoreAnswer(q, msg, idx), none}, idx); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path) //nolint:gosec // the test's own file
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var got []answerRecord
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var r answerRecord
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	if len(got) != 2 {
		t.Fatalf("%d lines, want one per answer", len(got))
	}
	h, n := got[0], got[1]
	if h.ID != "h01" || h.Answer != "An answer.[1]" || h.Status != "done" || h.ToolRounds != 2 || h.Usage.CostMicroUSD != 1500 {
		t.Errorf("h01 = %+v", h)
	}
	if len(h.Citations) != 1 || h.Citations[0].CitedText != "text" || strings.Join(h.Citations[0].Sections, "|") != "Doc > A" {
		t.Errorf("h01 citations = %+v", h.Citations)
	}
	if h.StrictHit || !h.SectionHit || strings.Join(h.ExpectSections, "|") != "Doc > A" {
		t.Errorf("h01 scores = strict %v, section %v, expects %v", h.StrictHit, h.SectionHit, h.ExpectSections)
	}
	if n.Citations == nil || len(n.Citations) != 0 || !n.Refused {
		t.Errorf("n01 = %+v, want a refusal with an empty citations list", n)
	}
}

func TestChatTableReportsBothHitRates(t *testing.T) {
	t.Parallel()
	idx, first, _ := twoSections()
	q := query{ID: "h01", Category: "howto", Expect: []string{"a1"}}
	report := chatTable([]answered{scoreAnswer(q, citing(first, "a2"), idx)}, "m", "https://example.test")
	for _, want := range []string{"| section hit rate | strict hit rate |", "| howto | 1 | 100% (1/1) | 1.00 | 0.00 |", "**Section** (the headline)", "**Strict**"} {
		if !strings.Contains(report, want) {
			t.Errorf("report lacks %q:\n%s", want, report)
		}
	}
}
