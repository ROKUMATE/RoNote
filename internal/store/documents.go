package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
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
	err = s.inTx(ctx, func(tx *Store) error {
		row, err := tx.q.CreateDocumentIfAbsent(ctx, sqlcdb.CreateDocumentIfAbsentParams{
			OwnerID:      d.OwnerID,
			SourceID:     d.SourceID,
			ExtractionID: d.ExtractionID,
			Title:        d.Content.Title,
			Content:      content,
		})
		if err != nil {
			return mapErr("create document", err)
		}
		id, created = row.ID, row.Created
		if !created {
			return nil
		}
		return tx.changed(ctx, d.OwnerID, id)
	})
	return id, created, err
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

// VersionConflictError means a save was made from an old version of a
// document or an annotation. Current is the version now.
type VersionConflictError struct {
	Current int
}

func (e *VersionConflictError) Error() string {
	return fmt.Sprintf("the version is now %d", e.Current)
}

// SaveDocument replaces the content of owner's document id if it is still at
// version, and returns the new version. It returns a *VersionConflictError
// when the document has moved on, and ErrNotFound when owner has no such
// document.
func (s *Store) SaveDocument(ctx context.Context, owner, id uuid.UUID, content *doccontent.Doc, version int) (int, error) {
	data, err := json.Marshal(content)
	if err != nil {
		return 0, fmt.Errorf("save document: %w", err)
	}
	var saved int32
	err = s.inTx(ctx, func(tx *Store) error {
		var err error
		saved, err = tx.q.SaveDocument(ctx, sqlcdb.SaveDocumentParams{
			Content: data, OwnerID: owner, ID: id, Version: int32(min(max(version, 0), math.MaxInt32)), //nolint:gosec // clamped
		})
		if err == nil {
			return tx.changed(ctx, owner, id)
		}
		if !errors.Is(mapErr("save document", err), ErrNotFound) {
			return mapErr("save document", err)
		}
		current, err := tx.q.DocumentVersion(ctx, sqlcdb.DocumentVersionParams{OwnerID: owner, ID: id})
		if err != nil {
			return mapErr("document version", err)
		}
		return &VersionConflictError{Current: int(current)}
	})
	if err != nil {
		return 0, err
	}
	return int(saved), nil
}

// IndexState is what indexing a document reads: its title and content,
// with the sequence it is at and the one its chunks were made from.
type IndexState struct {
	Title      string
	Content    *doccontent.Doc
	IndexSeq   int64
	IndexedSeq int64
}

// GetIndexState returns owner's document's IndexState, or ErrNotFound.
func (s *Store) GetIndexState(ctx context.Context, owner, id uuid.UUID) (IndexState, error) {
	row, err := s.q.GetIndexState(ctx, sqlcdb.GetIndexStateParams{OwnerID: owner, ID: id})
	if err != nil {
		return IndexState{}, mapErr("index state", err)
	}
	var content doccontent.Doc
	if err := json.Unmarshal(row.Content, &content); err != nil {
		return IndexState{}, fmt.Errorf("index state: %w", err)
	}
	return IndexState{Title: row.Title, Content: &content, IndexSeq: row.IndexSeq, IndexedSeq: row.IndexedSeq}, nil
}

// MarkIndexed records that owner's document's chunks are made from seq; an
// earlier seq never replaces a later one.
func (s *Store) MarkIndexed(ctx context.Context, owner, id uuid.UUID, seq int64) error {
	if err := s.q.MarkIndexed(ctx, sqlcdb.MarkIndexedParams{OwnerID: owner, ID: id, Seq: seq}); err != nil {
		return mapErr("mark indexed", err)
	}
	return nil
}

// Behind is a document whose chunks need making again.
type Behind struct {
	OwnerID, ID uuid.UUID
	IndexSeq    int64
}

// DocumentsBehind returns, across every user, up to limit documents whose
// chunks are behind their content or were embedded by a model other than
// model, most recently changed first.
func (s *Store) DocumentsBehind(ctx context.Context, model string, limit int) ([]Behind, error) {
	rows, err := s.q.DocumentsBehind(ctx, sqlcdb.DocumentsBehindParams{Model: model, MaxRows: int32(min(max(limit, 0), math.MaxInt32))}) //nolint:gosec // clamped
	if err != nil {
		return nil, mapErr("documents behind", err)
	}
	out := make([]Behind, len(rows))
	for i, r := range rows {
		out[i] = Behind{OwnerID: r.OwnerID, ID: r.ID, IndexSeq: r.IndexSeq}
	}
	return out, nil
}

// ResolveDocuments returns, for each of urls that has one, the id of owner's
// document for it, matched as FindOwnerDocumentForURL matches one URL. urls
// must be canonical.
func (s *Store) ResolveDocuments(ctx context.Context, owner uuid.UUID, urls []string) (map[string]uuid.UUID, error) {
	rows, err := s.q.ResolveDocuments(ctx, sqlcdb.ResolveDocumentsParams{OwnerID: owner, Urls: urls})
	if err != nil {
		return nil, mapErr("resolve documents", err)
	}
	found := make(map[string]uuid.UUID, len(rows))
	for _, r := range rows {
		found[r.URL] = r.DocumentID
	}
	return found, nil
}

// AddDocument stores doc as fetched from url and makes it owner's document,
// in one transaction: what an import does once it has the page, without
// fetching. The eval and tests seed libraries with it.
func (s *Store) AddDocument(ctx context.Context, owner uuid.UUID, url string, doc *ir.Doc) (uuid.UUID, error) {
	body, err := json.Marshal(doc)
	if err != nil {
		return uuid.Nil, fmt.Errorf("add document: %w", err)
	}
	sum := sha256.Sum256(body)
	var id uuid.UUID
	err = s.inTx(ctx, func(tx *Store) error {
		src, err := tx.UpsertSource(ctx, Source{CanonicalURL: url, FinalURL: url, LastFetchedAt: doc.Source.FetchedAt})
		if err != nil {
			return err
		}
		snap, err := tx.SaveSnapshot(ctx, Snapshot{
			SourceID: src.ID, BodySHA256: hex.EncodeToString(sum[:]), Body: body, ContentType: "application/json", FetchedAt: doc.Source.FetchedAt,
		})
		if err != nil {
			return err
		}
		ext, err := tx.SaveExtraction(ctx, snap, doc)
		if err != nil {
			return err
		}
		id, _, err = tx.CreateDocumentIfAbsent(ctx, NewDocument{OwnerID: owner, SourceID: src.ID, ExtractionID: ext.ID, Content: ext.Doc})
		return err
	})
	return id, err
}
