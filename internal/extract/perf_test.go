//go:build perf

package extract_test

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/rokumate/ronote/internal/extract"
	"github.com/rokumate/ronote/internal/ir"
)

// TestPerfExtractStaysLinear guards against inputs that made extraction
// quadratic. Pages come from user-supplied URLs, so a slow path is a denial
// of service. It checks growth, not wall time: doubling the input must not
// much more than double the time. Linear work takes about 2x; the quadratic
// versions took about 4x.
//
// Timings only mean something on a quiet machine, so this runs alone: the
// perf build tag keeps it out of make test and make test-integration, and
// make test-perf runs it one package at a time, without the race detector.
// Every timing test is named TestPerf..., which is what make test-perf runs.
func TestPerfExtractStaysLinear(t *testing.T) {
	tests := []struct {
		name   string
		repeat string
		count  int
	}{
		{name: "many identical headings", repeat: "<h2>Same</h2>", count: 40000},
		{name: "many text nodes in one paragraph", repeat: "x <span></span>", count: 300000},
		{name: "many line breaks in one paragraph", repeat: "x <br>", count: 300000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			small := fastestExtract(t, tt.repeat, tt.count)
			large := fastestExtract(t, tt.repeat, 2*tt.count)
			t.Logf("%d repeats: %v, %d: %v", tt.count, small, 2*tt.count, large)
			if ratio := float64(large) / float64(small); ratio > 3 {
				t.Errorf("doubling the input took %.1fx as long (%v, then %v); want about 2x", ratio, small, large)
			}
		})
	}
}

// fastestExtract returns the best of five extraction times of a page whose
// main element holds repeat count times. The fastest run leaves out GC
// pauses and scheduling noise.
func fastestExtract(t *testing.T, repeat string, count int) time.Duration {
	t.Helper()
	html := "<main>" + strings.Repeat(repeat, count) + "</main>"
	best := time.Duration(math.MaxInt64)
	for range 5 {
		start := time.Now()
		if _, err := extract.Extract(strings.NewReader(html), ir.Source{FinalURL: pageURL}); err != nil {
			t.Fatal(err)
		}
		best = min(best, time.Since(start))
	}
	return best
}
