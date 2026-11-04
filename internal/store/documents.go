package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/doccontent"
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
	Content *doccontent.Doc
	// Warnings are the extraction's: what the extractor was unsure of.
	Warnings []ir.Warning
}

// NewDocument is what a document is created from: the extraction of a
// source, whose blocks become the content as unedited source blocks.
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
	content, err := json.Marshal(doccontent.FromIR(d.Content))
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
	var content doccontent.Doc
	if err := json.Unmarshal(row.Content, &content); err != nil {
		return Document{}, fmt.Errorf("document %s: %w", row.ID, err)
	}
	var warnings []ir.Warning
	if err := json.Unmarshal(row.Warnings, &warnings); err != nil {
		return Document{}, fmt.Errorf("document %s warnings: %w", row.ID, err)
	}
	return Document{
		DocumentSummary: DocumentSummary{
			ID: row.ID, Title: row.Title, SourceURL: row.SourceURL, Version: int(row.Version),
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		},
		Content:  &content,
		Warnings: warnings,
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

// DocumentExtraction returns the IR of the extraction owner's document id
// was made from, or ErrNotFound, also when the document is someone else's.
func (s *Store) DocumentExtraction(ctx context.Context, owner, id uuid.UUID) (*ir.Doc, error) {
	raw, err := s.q.DocumentExtraction(ctx, sqlcdb.DocumentExtractionParams{OwnerID: owner, ID: id})
	if err != nil {
		return nil, mapErr("document extraction", err)
	}
	var doc ir.Doc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("extraction of document %s: %w", id, err)
	}
	return &doc, nil
}
