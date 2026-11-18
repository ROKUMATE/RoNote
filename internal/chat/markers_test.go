package chat

import "testing"

func TestCountMarkers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
		want int
	}{
		{name: "none", text: "Channels connect goroutines.", want: 0},
		{name: "a number", text: "Channels connect goroutines.[1]", want: 1},
		{name: "after a space", text: "Channels connect goroutines [1].", want: 1},
		{name: "in parentheses", text: "A 404 is a client error ([1-6]) and raises ([2-2]).", want: 2},
		{name: "a range with an en dash", text: "See [1–3].", want: 1},
		{name: "a list", text: "See [1, 2] and [3,4].", want: 2},
		{name: "side by side", text: "Both agree.[1][2]", want: 2},
		{name: "at the start", text: "[1] says so.", want: 1},
		{name: "an index after a word", text: "Use items[0] and grid[1][2].", want: 1},
		{name: "not a number", text: "See [a] and [].", want: 0},
		{name: "inline code", text: "Write `[1]` or `xs[0]` to index.", want: 0},
		{name: "a fence", text: "Do this:\n```go\nfmt.Println(os.Args[1], [1])\n```\nThen see [2].", want: 1},
		{name: "an unterminated fence", text: "Do this:\n```go\nx := []int{1}[0] [1]", want: 0},
		{name: "a fence with the text after it and before it", text: "[1] first\n```\n[2]\n```\n[3] last", want: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := countMarkers(tt.text); got != tt.want {
				t.Errorf("countMarkers(%q) = %d, want %d", tt.text, got, tt.want)
			}
		})
	}
}
