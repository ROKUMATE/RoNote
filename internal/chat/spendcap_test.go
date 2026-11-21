package chat

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// The host's alert watcher matches the cap's message in the journal
// (docs/adr/0015-operations.md): once a UTC day, however many questions the
// cap stops.
func TestSpendCapLogsOnceADay(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&out, nil))
	var l spendCapLog
	day := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	for _, at := range []time.Time{day, day.Add(3 * time.Hour), day.Add(14*time.Hour + 59*time.Minute)} {
		l.reached(logger, at, 200_000)
	}
	l.reached(logger, day.Add(15*time.Hour), 200_000) // 00:00 UTC, the next day
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("logged %d lines, want 2, one a day:\n%s", len(lines), out.String())
	}
	for i, want := range []string{`"day":"2026-11-21"`, `"day":"2026-11-22"`} {
		if !strings.Contains(lines[i], `"msg":"`+SpendCapReached+`"`) || !strings.Contains(lines[i], want) || !strings.Contains(lines[i], `"cap_usd":0.2`) {
			t.Errorf("line %d = %s, want the message, %s and the cap", i, lines[i], want)
		}
	}
}
