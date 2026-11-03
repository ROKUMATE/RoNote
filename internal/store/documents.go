package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/ir"
	"github.com/rokumate/ronote/internal/store/sqlcdb"
)

// DocumentSummary is a document without its content.
type DocumentSummary struct {
	ID    uuid.UUID
	Title string
	// SourceURL is the canonical URL of the page the document came from.
	SourceURL string
	// Version counts edits, for optimistic concurrency; it starts at 1.
	Version   int
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Document is a user's own copy of an imported page.
type Document struct {
	DocumentSummary
	Content *ir.Doc
}

// NewDocument is what a document is created from: the extraction of a
// source, whose IR becomes the content.
type NewDocument struct {
	OwnerID      uuid.UUID
	SourceID     uuid.UUID
	ExtractionID uuid.UUID
	Content      *ir.Doc
}

// DocumentCursor marks a position in a document list: the created_at and ID
// of the last document of the previous page.
type DocumentCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// listStart is a cursor before every document.
var listStart = DocumentCursor{CreatedAt: time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC), ID: uuid.Max}

// CreateDocumentIfAbsent creates owner's document for a source, or returns
// the one they already have, so retried imports never make a second one.
// created reports whether this call made it.
func (s *Store) CreateDocumentIfAbsent(ctx context.Context, d NewDocument) (id uuid.UUID, created bool, err error) {
	content, err := json.Marshal(d.Content)
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("create document: %w", err)
	}
	row, err := s.q.CreateDocumentIfAbsent(ctx, sqlcdb.CreateDocumentIfAbsentParams{
		OwnerID:      d.OwnerID,
		SourceID:     d.SourceID,
		ExtractionID: d.ExtractionID,
		Title:        d.Content.Title,
		Content:      content,
	})
	if err != nil {
		return uuid.Nil, false, mapErr("create document", err)
	}
	return row.ID, row.Created, nil
}

// FindOwnerDocumentForURL returns the ID of owner's document for url: one
// whose source has url as canonical or final URL, or one an earlier import
// of url produced. It returns ErrNotFound when owner has none.
func (s *Store) FindOwnerDocumentForURL(ctx context.Context, owner uuid.UUID, url string) (uuid.UUID, error) {
	id, err := s.q.FindOwnerDocumentForURL(ctx, sqlcdb.FindOwnerDocumentForURLParams{OwnerID: owner, URL: url})
	if err != nil {
		return uuid.Nil, mapErr("find document for URL", err)
	}
	return id, nil
}

// GetDocument returns owner's document id with its content, or ErrNotFound,
// also when it belongs to someone else.
func (s *Store) GetDocument(ctx context.Context, owner, id uuid.UUID) (Document, error) {
	row, err := s.q.GetDocument(ctx, sqlcdb.GetDocumentParams{OwnerID: owner, ID: id})
	if err != nil {
		return Document{}, mapErr("get document", err)
	}
	var content ir.Doc
	if err := json.Unmarshal(row.Content, &content); err != nil {
		return Document{}, fmt.Errorf("document %s: %w", row.ID, err)
	}
	return Document{
		DocumentSummary: DocumentSummary{
			ID: row.ID, Title: row.Title, SourceURL: row.SourceURL, Version: int(row.Version),
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		},
		Content: &content,
	}, nil
}

// ListDocuments returns up to limit of owner's documents, newest first,
// starting after the cursor, or from the newest when after is nil.
func (s *Store) ListDocuments(ctx context.Context, owner uuid.UUID, after *DocumentCursor, limit int) ([]DocumentSummary, error) {
	from := listStart
	if after != nil {
		from = *after
	}
	rows, err := s.q.ListDocuments(ctx, sqlcdb.ListDocumentsParams{
		OwnerID: owner, BeforeCreatedAt: from.CreatedAt, BeforeID: from.ID,
		MaxRows: int32(min(max(limit, 0), 1000)), //nolint:gosec // clamped to 0-1000
	})
	if err != nil {
		return nil, mapErr("list documents", err)
	}
	docs := make([]DocumentSummary, len(rows))
	for i, r := range rows {
		docs[i] = DocumentSummary{
			ID: r.ID, Title: r.Title, SourceURL: r.SourceURL, Version: int(r.Version),
			CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		}
	}
	return docs, nil
}
