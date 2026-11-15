package main

import (
	"errors"
	"fmt"
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
			all, complete, err := askAll(queriesN(10), chatRun{limit: tt.limit, budget: tt.budget}, costing(tt.cost, &asked), func(string) {})
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
	failing := func(q query) (store.Message, error) { return store.Message{}, errors.New("model down") }
	if _, _, err := askAll(queriesN(3), chatRun{budget: 1_000_000}, failing, func(string) {}); err == nil {
		t.Error("askAll went on after an error")
	}
}

func TestProjection(t *testing.T) {
	t.Parallel()
	asked := 0
	all, _, err := askAll(queriesN(10), chatRun{limit: 10, budget: 1_500_000}, costing(2_500, &asked), func(string) {})
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
