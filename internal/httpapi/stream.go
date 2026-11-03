package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/events"
	"github.com/rokumate/ronote/internal/httpapi/gen"
	"github.com/rokumate/ronote/internal/store"
)

// heartbeat is how often an idle event stream sends a comment line, so
// proxies and browsers keep the connection open.
const heartbeat = 15 * time.Second

// Events is where event streams hear about status changes. *events.Broker
// implements it; it is an interface so handler tests can drive it.
type Events interface {
	Subscribe(id uuid.UUID) (<-chan events.Event, func())
}

// statusOrder ranks statuses so a stream only ever moves forward.
var statusOrder = map[store.ImportStatus]int{
	store.ImportQueued: 0, store.ImportFetching: 1, store.ImportExtracting: 2, store.ImportDone: 3, store.ImportFailed: 3,
}

// importEvents streams an import's status as server-sent events: the
// current status at once, then each later one, ending after done or failed.
// It subscribes before reading the import, so no change can fall between the
// two.
func importEvents(backend Backend, feed Events, done <-chan struct{}, every time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		id, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			writeError(w, r, http.StatusBadRequest, gen.ErrorCodeInvalidParameter, "id is not a UUID")
			return
		}
		owner := userFrom(ctx)
		updates, unsubscribe := feed.Subscribe(id)
		defer unsubscribe()

		imp, err := backend.GetImport(ctx, owner, id)
		switch {
		case errors.Is(err, store.ErrNotFound):
			writeError(w, r, http.StatusNotFound, gen.ErrorCodeNotFound, "no such import")
			return
		case err != nil:
			writeError(w, r, http.StatusInternalServerError, gen.ErrorCodeInternalError, "internal server error")
			return
		}

		rc := http.NewResponseController(w)
		// The server's write timeout is for ordinary responses; a stream
		// lasts as long as the import. Writers that cannot change the
		// deadline (as in tests) have none to lift.
		_ = rc.SetWriteDeadline(time.Time{})
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		if send(w, rc, imp) != nil || imp.Status.Terminal() {
			return
		}

		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil || rc.Flush() != nil {
					return
				}
			case ev := <-updates:
				next, err := apply(ctx, backend, owner, imp, ev)
				if err != nil {
					return
				}
				if statusOrder[next.Status] <= statusOrder[imp.Status] {
					continue
				}
				imp = next
				if send(w, rc, imp) != nil || imp.Status.Terminal() {
					return
				}
			}
		}
	}
}

// apply returns the import after ev: as the event says, or as stored when
// the event asks for a resync.
func apply(ctx context.Context, backend Backend, owner uuid.UUID, imp store.Import, ev events.Event) (store.Import, error) {
	if ev.Resync {
		return backend.GetImport(ctx, owner, imp.ID)
	}
	if ev.OwnerID != owner {
		return imp, nil
	}
	imp.Status, imp.ErrorCode, imp.DocumentID = store.ImportStatus(ev.Status), ev.ErrorCode, ev.DocumentID
	return imp, nil
}

// send writes one status event and flushes it to the client.
func send(w http.ResponseWriter, rc *http.ResponseController, imp store.Import) error {
	ev := gen.ImportEvent{Status: gen.ImportStatus(imp.Status)}
	if imp.ErrorCode != "" {
		code := gen.ImportErrorCode(imp.ErrorCode)
		ev.ErrorCode = &code
	}
	if imp.DocumentID != uuid.Nil {
		ev.DocumentID = &imp.DocumentID
	}
	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: status\ndata: %s\n\n", data); err != nil {
		return err
	}
	return rc.Flush()
}
