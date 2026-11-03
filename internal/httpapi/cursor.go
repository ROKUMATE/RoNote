package httpapi

import (
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/store"
)

var errBadCursor = errors.New("invalid cursor")

// encodeCursor makes the opaque next_cursor for a page that ends at c.
func encodeCursor(c store.DocumentCursor) string {
	return base64.RawURLEncoding.EncodeToString([]byte(c.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + c.ID.String()))
}

// decodeCursor reads a cursor encodeCursor made.
func decodeCursor(s string) (store.DocumentCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return store.DocumentCursor{}, errBadCursor
	}
	at, id, ok := strings.Cut(string(raw), "|")
	if !ok {
		return store.DocumentCursor{}, errBadCursor
	}
	createdAt, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return store.DocumentCursor{}, errBadCursor
	}
	docID, err := uuid.Parse(id)
	if err != nil {
		return store.DocumentCursor{}, errBadCursor
	}
	return store.DocumentCursor{CreatedAt: createdAt, ID: docID}, nil
}
