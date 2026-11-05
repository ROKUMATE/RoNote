package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/rokumate/ronote/internal/annotation"
	"github.com/rokumate/ronote/internal/doccontent"
	"github.com/rokumate/ronote/internal/store/sqlcdb"
)

// Annotation is a highlight in one block of a user's document, with an
// optional note (docs/adr/0007-annotations-and-anchoring.md).
type Annotation struct {
	ID         uuid.UUID
	DocumentID uuid.UUID
	BlockID    string
	Kind       string
	Selector   annotation.Selector
	// Note is empty when the highlight has none.
	Note      string
	Version   int
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewAnnotation is an annotation to create.
type NewAnnotation struct {
	BlockID  string
	Kind     string
	Selector annotation.Selector
	Note     string
}

// AnnotationChange is what to change in an annotation made at Version.
// Nil fields stay as they are; BlockID and Selector change together.
type AnnotationChange struct {
	Version  int
	Note     *string
	BlockID  *string
	Selector *annotation.Selector
}

// Reanchor is a new selector for an annotation made at Version.
type Reanchor struct {
	ID       uuid.UUID
	Selector annotation.Selector
	Version  int
}

// ListAnnotations returns owner's annotations of document, oldest first, or
// ErrNotFound when owner has no such document.
func (s *Store) ListAnnotations(ctx context.Context, owner, document uuid.UUID) ([]Annotation, error) {
	if _, err := s.q.DocumentVersion(ctx, sqlcdb.DocumentVersionParams{OwnerID: owner, ID: document}); err != nil {
		return nil, mapErr("document of annotations", err)
	}
	rows, err := s.q.ListAnnotations(ctx, sqlcdb.ListAnnotationsParams{OwnerID: owner, DocumentID: document})
	if err != nil {
		return nil, mapErr("list annotations", err)
	}
	out := make([]Annotation, len(rows))
	for i, row := range rows {
		if out[i], err = fromRow(row); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// CreateAnnotation adds an annotation to owner's document after checking
// it against the document's content, which stays locked meanwhile. It
// returns an *annotation.Problem for a selector that does not fit or a full
// document, and ErrNotFound when owner has no such document.
func (s *Store) CreateAnnotation(ctx context.Context, owner, document uuid.UUID, a NewAnnotation) (Annotation, error) {
	var created Annotation
	err := s.InTx(ctx, func(tx *Store, _ pgx.Tx) error {
		content, err := tx.lockContent(ctx, owner, document)
		if err != nil {
			return err
		}
		if err := annotation.Check(content, a.BlockID, a.Selector); err != nil {
			return err
		}
		count, err := tx.q.CountAnnotations(ctx, sqlcdb.CountAnnotationsParams{OwnerID: owner, DocumentID: document})
		if err != nil {
			return mapErr("count annotations", err)
		}
		if count >= annotation.MaxPerDocument {
			return annotation.LimitReached()
		}
		selector, err := json.Marshal(a.Selector)
		if err != nil {
			return fmt.Errorf("create annotation: %w", err)
		}
		row, err := tx.q.CreateAnnotation(ctx, sqlcdb.CreateAnnotationParams{
			OwnerID: owner, DocumentID: document, BlockID: a.BlockID, Kind: a.Kind,
			Selector: selector, Note: noteText(a.Note),
		})
		if err != nil {
			return mapErr("create annotation", err)
		}
		created, err = fromRow(row)
		return err
	})
	return created, err
}

// UpdateAnnotation applies change to owner's annotation id if it is still
// at change.Version, checking a new selector against the document's
// content. It returns a *VersionConflictError when the annotation has moved
// on, an *annotation.Problem for a selector that does not fit, and
// ErrNotFound when owner has no such annotation.
func (s *Store) UpdateAnnotation(ctx context.Context, owner, id uuid.UUID, change AnnotationChange) (Annotation, error) {
	var updated Annotation
	err := s.InTx(ctx, func(tx *Store, _ pgx.Tx) error {
		current, err := tx.getAnnotation(ctx, owner, id)
		if err != nil {
			return err
		}
		next := current
		if change.Note != nil {
			next.Note = *change.Note
		}
		if change.BlockID != nil && change.Selector != nil {
			content, err := tx.lockContent(ctx, owner, current.DocumentID)
			if err != nil {
				return err
			}
			if err := annotation.Check(content, *change.BlockID, *change.Selector); err != nil {
				return err
			}
			next.BlockID, next.Selector = *change.BlockID, *change.Selector
		}
		updated, err = tx.saveAnnotation(ctx, owner, next, change.Version)
		return err
	})
	return updated, err
}

// ReanchorAnnotations saves new selectors for owner's annotations of
// document, all or none, checking each against the document's content. It
// returns ErrNotFound when an annotation is not one of owner's document, a
// *VersionConflictError when one has moved on, and an *annotation.Problem
// for a selector that does not fit.
func (s *Store) ReanchorAnnotations(ctx context.Context, owner, document uuid.UUID, moves []Reanchor) ([]Annotation, error) {
	var saved []Annotation
	err := s.InTx(ctx, func(tx *Store, _ pgx.Tx) error {
		content, err := tx.lockContent(ctx, owner, document)
		if err != nil {
			return err
		}
		saved = make([]Annotation, 0, len(moves))
		for _, m := range moves {
			current, err := tx.getAnnotation(ctx, owner, m.ID)
			if err != nil {
				return err
			}
			if current.DocumentID != document {
				return ErrNotFound
			}
			if err := annotation.Check(content, current.BlockID, m.Selector); err != nil {
				return err
			}
			current.Selector = m.Selector
			a, err := tx.saveAnnotation(ctx, owner, current, m.Version)
			if err != nil {
				return err
			}
			saved = append(saved, a)
		}
		return nil
	})
	return saved, err
}

// DeleteAnnotation removes owner's annotation id, or returns ErrNotFound.
func (s *Store) DeleteAnnotation(ctx context.Context, owner, id uuid.UUID) error {
	n, err := s.q.DeleteAnnotation(ctx, sqlcdb.DeleteAnnotationParams{OwnerID: owner, ID: id})
	if err != nil {
		return mapErr("delete annotation", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// lockContent returns owner's document's content, locked until the
// transaction ends, or ErrNotFound.
func (s *Store) lockContent(ctx context.Context, owner, document uuid.UUID) (*doccontent.Doc, error) {
	raw, err := s.q.LockDocumentContent(ctx, sqlcdb.LockDocumentContentParams{OwnerID: owner, ID: document})
	if err != nil {
		return nil, mapErr("lock document", err)
	}
	var content doccontent.Doc
	if err := json.Unmarshal(raw, &content); err != nil {
		return nil, fmt.Errorf("content of document %s: %w", document, err)
	}
	return &content, nil
}

func (s *Store) getAnnotation(ctx context.Context, owner, id uuid.UUID) (Annotation, error) {
	row, err := s.q.GetAnnotation(ctx, sqlcdb.GetAnnotationParams{OwnerID: owner, ID: id})
	if err != nil {
		return Annotation{}, mapErr("get annotation", err)
	}
	return fromRow(row)
}

// saveAnnotation writes a's block, selector and note if the stored
// annotation is still at version.
func (s *Store) saveAnnotation(ctx context.Context, owner uuid.UUID, a Annotation, version int) (Annotation, error) {
	selector, err := json.Marshal(a.Selector)
	if err != nil {
		return Annotation{}, fmt.Errorf("save annotation: %w", err)
	}
	row, err := s.q.UpdateAnnotation(ctx, sqlcdb.UpdateAnnotationParams{
		BlockID: a.BlockID, Selector: selector, Note: noteText(a.Note),
		OwnerID: owner, ID: a.ID, Version: int32(min(max(version, 0), math.MaxInt32)), //nolint:gosec // clamped
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Annotation{}, &VersionConflictError{Current: a.Version}
	}
	if err != nil {
		return Annotation{}, mapErr("save annotation", err)
	}
	return fromRow(row)
}

func noteText(note string) pgtype.Text {
	return pgtype.Text{String: note, Valid: note != ""}
}

func fromRow(row sqlcdb.Annotation) (Annotation, error) {
	var selector annotation.Selector
	if err := json.Unmarshal(row.Selector, &selector); err != nil {
		return Annotation{}, fmt.Errorf("selector of annotation %s: %w", row.ID, err)
	}
	return Annotation{
		ID: row.ID, DocumentID: row.DocumentID, BlockID: row.BlockID, Kind: row.Kind,
		Selector: selector, Note: row.Note.String, Version: int(row.Version),
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}, nil
}
