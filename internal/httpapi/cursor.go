package httpapi

import (
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var errBadCursor = errors.New("invalid cursor")

// encodeCursor makes the opaque next_cursor for a page whose last row has
// the time at and id: a document's created_at, a conversation's
// updated_at.
func encodeCursor(at time.Time, id uuid.UUID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(at.UTC().Format(time.RFC3339Nano) + "|" + id.String()))
}

// decodeCursor reads a cursor encodeCursor made.
func decodeCursor(s string) (time.Time, uuid.UUID, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, uuid.Nil, errBadCursor
	}
	rawAt, rawID, ok := strings.Cut(string(raw), "|")
	if !ok {
		return time.Time{}, uuid.Nil, errBadCursor
	}
	at, err := time.Parse(time.RFC3339Nano, rawAt)
	if err != nil {
		return time.Time{}, uuid.Nil, errBadCursor
	}
	id, err := uuid.Parse(rawID)
	if err != nil {
		return time.Time{}, uuid.Nil, errBadCursor
	}
	return at, id, nil
}
