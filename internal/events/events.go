// Package events delivers import status changes to the API process. Every
// change notifies the Postgres channel import_status as it commits; one
// connection per process listens there and Broker fans each notification
// out in memory to whoever follows that import.
package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Channel is the Postgres channel the imports table's trigger notifies.
const Channel = "import_status"

// Reconnect delays after the listening connection is lost.
const (
	firstRetryDelay = 100 * time.Millisecond
	maxRetryDelay   = 5 * time.Second
)

// buffered is how many events a subscriber's channel holds. An import
// changes status at most four times, so a reader never falls this far
// behind.
const buffered = 16

// Event is one import status change, as the trigger sends it.
type Event struct {
	ImportID   uuid.UUID `json:"import_id"`
	OwnerID    uuid.UUID `json:"owner_id"`
	Status     string    `json:"status"`
	ErrorCode  string    `json:"error_code"`
	DocumentID uuid.UUID `json:"document_id"`
	// Resync is set when the listening connection was replaced: changes may
	// have been missed, so the import should be read again.
	Resync bool `json:"-"`
}

// Broker fans notifications out to subscribers by import.
type Broker struct {
	mu   sync.Mutex
	subs map[uuid.UUID]map[chan Event]struct{}
}

// NewBroker returns a Broker with no subscribers. Run Listen to feed it.
func NewBroker() *Broker {
	return &Broker{subs: map[uuid.UUID]map[chan Event]struct{}{}}
}

// Subscribe returns the events for import id from now on. Call cancel when
// done; the channel is never closed.
func (b *Broker) Subscribe(id uuid.UUID) (events <-chan Event, cancel func()) {
	ch := make(chan Event, buffered)
	b.mu.Lock()
	if b.subs[id] == nil {
		b.subs[id] = map[chan Event]struct{}{}
	}
	b.subs[id][ch] = struct{}{}
	b.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			delete(b.subs[id], ch)
			if len(b.subs[id]) == 0 {
				delete(b.subs, id)
			}
		})
	}
}

// subscribers counts the open subscriptions.
func (b *Broker) subscribers() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, subs := range b.subs {
		n += len(subs)
	}
	return n
}

// publish hands ev to the subscribers of its import without blocking.
func (b *Broker) publish(ev Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs[ev.ImportID] {
		select {
		case ch <- ev:
		default: // a reader this far behind re-reads the import when it catches up
		}
	}
}

// resyncAll tells every subscriber to read its import again.
func (b *Broker) resyncAll() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, subs := range b.subs {
		for ch := range subs {
			select {
			case ch <- Event{ImportID: id, Resync: true}:
			default:
			}
		}
	}
}

// Listen keeps one connection from pool listening on Channel and publishes
// what arrives, until ctx ends. A lost connection is replaced with backoff,
// and subscribers are told to resync, since changes may have been missed.
func (b *Broker) Listen(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) {
	delay := firstRetryDelay
	for {
		err := b.listenOnce(ctx, pool, logger)
		if ctx.Err() != nil {
			return
		}
		logger.WarnContext(ctx, "import event listener lost its connection", "error", err, "retry_in_ms", delay.Milliseconds())
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = min(delay*2, maxRetryDelay)
	}
}

func (b *Broker) listenOnce(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) error {
	acquired, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	// The connection is taken out of the pool for good: a LISTENing
	// connection must never be handed to another query.
	conn := acquired.Hijack()
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }() // closing a broken connection has nothing to report
	if _, err := conn.Exec(ctx, "LISTEN "+Channel); err != nil {
		return err
	}
	b.resyncAll()
	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		var ev Event
		if err := json.Unmarshal([]byte(n.Payload), &ev); err != nil {
			logger.ErrorContext(ctx, "bad import event", "payload", n.Payload, "error", err)
			continue
		}
		b.publish(ev)
	}
}
