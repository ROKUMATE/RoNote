package chat

import (
	"log/slog"
	"sync"
	"time"
)

// SpendCapReached is what the api logs the first time each UTC day the
// spend cap stops a question; the host's alert watcher matches it in the
// journal (docs/adr/0015-operations.md).
const SpendCapReached = "chat spend cap reached"

// spendCapLog remembers the UTC day it last logged, so a day's refusals
// after the first log nothing.
type spendCapLog struct {
	mu  sync.Mutex
	day string
}

func (l *spendCapLog) reached(logger *slog.Logger, now time.Time, capMicroUSD int64) {
	day := now.UTC().Format(time.DateOnly)
	l.mu.Lock()
	first := l.day != day
	l.day = day
	l.mu.Unlock()
	if first {
		logger.Warn(SpendCapReached, "day", day, "cap_usd", float64(capMicroUSD)/1e6)
	}
}
