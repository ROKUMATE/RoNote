package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/rokumate/ronote/internal/store/sqlcdb"
)

// ImportStatus is where an import is in the pipeline.
type ImportStatus string

// Import statuses, in pipeline order. Done and failed are terminal.
const (
	ImportQueued     ImportStatus = "queued"
	ImportFetching   ImportStatus = "fetching"
	ImportExtracting ImportStatus = "extracting"
	ImportDone       ImportStatus = "done"
	ImportFailed     ImportStatus = "failed"
)

// Terminal reports whether an import in status s is finished.
func (s ImportStatus) Terminal() bool {
	return s == ImportDone || s == ImportFailed
}

// Import is one user's request to import a URL. Every status change notifies
// the import_status channel when its transaction commits.
type Import struct {
	ID           uuid.UUID
	OwnerID      uuid.UUID
	RequestedURL string
	// CanonicalURL is the requested URL, canonicalized.
	CanonicalURL string
	Status       ImportStatus
	// ErrorCode is set when Status is failed.
	ErrorCode string
	// DocumentID is set when Status is done.
	DocumentID uuid.UUID
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func importFrom(row sqlcdb.Import) Import {
	return Import{
		ID:           row.ID,
		OwnerID:      row.OwnerID,
		RequestedURL: row.RequestedURL,
		CanonicalURL: row.CanonicalURL,
		Status:       ImportStatus(row.Status),
		ErrorCode:    row.ErrorCode.String,
		DocumentID:   row.DocumentID.UUID,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}
}

// CreateImport records a queued import of requestedURL for owner.
func (s *Store) CreateImport(ctx context.Context, owner uuid.UUID, requestedURL, canonicalURL string) (Import, error) {
	row, err := s.q.CreateImport(ctx, sqlcdb.CreateImportParams{OwnerID: owner, RequestedURL: requestedURL, CanonicalURL: canonicalURL})
	if err != nil {
		return Import{}, mapErr("create import", err)
	}
	return importFrom(row), nil
}

// GetImport returns owner's import id, or ErrNotFound, also when it belongs
// to someone else.
func (s *Store) GetImport(ctx context.Context, owner, id uuid.UUID) (Import, error) {
	row, err := s.q.GetImport(ctx, sqlcdb.GetImportParams{OwnerID: owner, ID: id})
	if err != nil {
		return Import{}, mapErr("get import", err)
	}
	return importFrom(row), nil
}

// SetImportStatus moves owner's unfinished import id to a working status. It
// returns ErrNotFound when the import is missing or already finished.
func (s *Store) SetImportStatus(ctx context.Context, owner, id uuid.UUID, status ImportStatus) (Import, error) {
	row, err := s.q.SetImportStatus(ctx, sqlcdb.SetImportStatusParams{OwnerID: owner, ID: id, Status: string(status)})
	if err != nil {
		return Import{}, mapErr("set import status", err)
	}
	return importFrom(row), nil
}

// FinishImport marks owner's import id done with document. Finishing it
// again with the same document is a no-op that succeeds; a failed or
// missing import gives ErrNotFound.
func (s *Store) FinishImport(ctx context.Context, owner, id, document uuid.UUID) (Import, error) {
	row, err := s.q.FinishImport(ctx, sqlcdb.FinishImportParams{
		OwnerID: owner, ID: id, DocumentID: uuid.NullUUID{UUID: document, Valid: true},
	})
	if err != nil {
		return Import{}, mapErr("finish import", err)
	}
	return importFrom(row), nil
}

// FailImport marks owner's unfinished import id failed with code. A finished
// or missing import gives ErrNotFound.
func (s *Store) FailImport(ctx context.Context, owner, id uuid.UUID, code string) (Import, error) {
	row, err := s.q.FailImport(ctx, sqlcdb.FailImportParams{OwnerID: owner, ID: id, ErrorCode: pgtype.Text{String: code, Valid: true}})
	if err != nil {
		return Import{}, mapErr("fail import", err)
	}
	return importFrom(row), nil
}

// LatestImportForDocument returns owner's most recent import that produced
// document, or ErrNotFound.
func (s *Store) LatestImportForDocument(ctx context.Context, owner, document uuid.UUID) (Import, error) {
	row, err := s.q.LatestImportForDocument(ctx, sqlcdb.LatestImportForDocumentParams{
		OwnerID: owner, DocumentID: uuid.NullUUID{UUID: document, Valid: true},
	})
	if err != nil {
		return Import{}, mapErr("latest import for document", err)
	}
	return importFrom(row), nil
}
