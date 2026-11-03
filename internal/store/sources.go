package store

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/ir"
	"github.com/rokumate/ronote/internal/store/sqlcdb"
)

// maxBody is the largest page body a snapshot holds once decompressed. The
// fetcher stops at 5 MiB, so a larger stored body means corrupt data.
const maxBody = 5 << 20

// Source is a fetched web page, one per canonical URL, shared by all users.
type Source struct {
	ID           uuid.UUID
	CanonicalURL string
	FinalURL     string
	// ETag and LastModified come from the last fetch, for conditional
	// requests; "" when the server sent none.
	ETag         string
	LastModified string
	// LastFetchedAt is when the newest snapshot was fetched or a 304
	// confirmed it.
	LastFetchedAt time.Time
}

// Snapshot is one version of a source's body, decoded to UTF-8.
type Snapshot struct {
	ID       uuid.UUID
	SourceID uuid.UUID
	// BodySHA256 is the hex SHA-256 of the body as the server sent it.
	BodySHA256  string
	Body        []byte
	ContentType string
	FetchedAt   time.Time
}

// Extraction is the IR an extractor version made from a snapshot.
type Extraction struct {
	ID  uuid.UUID
	Doc *ir.Doc
}

// FindSourceForURL returns the source whose canonical URL, or else whose
// final URL, is url, or ErrNotFound.
func (s *Store) FindSourceForURL(ctx context.Context, url string) (Source, error) {
	row, err := s.q.FindSourceForURL(ctx, url)
	if err != nil {
		return Source{}, mapErr("find source", err)
	}
	return Source(row), nil
}

// UpsertSource records a fetch of the source with src.CanonicalURL,
// creating it the first time, and returns it.
func (s *Store) UpsertSource(ctx context.Context, src Source) (Source, error) {
	row, err := s.q.UpsertSource(ctx, sqlcdb.UpsertSourceParams{
		CanonicalURL:  src.CanonicalURL,
		FinalURL:      src.FinalURL,
		ETag:          src.ETag,
		LastModified:  src.LastModified,
		LastFetchedAt: src.LastFetchedAt,
	})
	if err != nil {
		return Source{}, mapErr("upsert source", err)
	}
	return Source(row), nil
}

// TouchSource records that a 304 at fetchedAt confirmed the source's newest
// snapshot.
func (s *Store) TouchSource(ctx context.Context, id uuid.UUID, fetchedAt time.Time) error {
	if err := s.q.TouchSource(ctx, sqlcdb.TouchSourceParams{ID: id, FetchedAt: fetchedAt}); err != nil {
		return mapErr("touch source", err)
	}
	return nil
}

// NewestSnapshot returns the source's most recently fetched snapshot, or
// ErrNotFound.
func (s *Store) NewestSnapshot(ctx context.Context, sourceID uuid.UUID) (Snapshot, error) {
	row, err := s.q.NewestSnapshot(ctx, sourceID)
	if err != nil {
		return Snapshot{}, mapErr("newest snapshot", err)
	}
	body, err := gunzip(row.BodyGzip)
	if err != nil {
		return Snapshot{}, fmt.Errorf("newest snapshot %s: %w", row.ID, err)
	}
	return Snapshot{
		ID:          row.ID,
		SourceID:    row.SourceID,
		BodySHA256:  row.BodySHA256,
		Body:        body,
		ContentType: row.ContentType,
		FetchedAt:   row.FetchedAt,
	}, nil
}

// SaveSnapshot stores snap's body gzip-compressed and returns its ID. When
// the source already has a snapshot with the same body hash, it returns
// that one's ID and stores nothing.
func (s *Store) SaveSnapshot(ctx context.Context, snap Snapshot) (uuid.UUID, error) {
	compressed, err := gzipBytes(snap.Body)
	if err != nil {
		return uuid.Nil, fmt.Errorf("save snapshot: %w", err)
	}
	id, err := s.q.SaveSnapshot(ctx, sqlcdb.SaveSnapshotParams{
		SourceID:    snap.SourceID,
		BodySHA256:  snap.BodySHA256,
		BodyGzip:    compressed,
		ContentType: snap.ContentType,
		FetchedAt:   snap.FetchedAt,
	})
	if err != nil {
		return uuid.Nil, mapErr("save snapshot", err)
	}
	return id, nil
}

// SaveExtraction stores doc as the extraction of a snapshot by doc's
// extractor and version. When that extraction already exists, it returns
// the stored one, whose IR then wins over doc.
func (s *Store) SaveExtraction(ctx context.Context, snapshotID uuid.UUID, doc *ir.Doc) (Extraction, error) {
	data, err := json.Marshal(doc)
	if err != nil {
		return Extraction{}, fmt.Errorf("save extraction: %w", err)
	}
	row, err := s.q.SaveExtraction(ctx, sqlcdb.SaveExtractionParams{
		SnapshotID:       snapshotID,
		ExtractorName:    doc.Extractor.Name,
		ExtractorVersion: doc.Extractor.Version,
		IRVersion:        int32(doc.IRVersion), //nolint:gosec // ir.Version is a small constant
		IR:               data,
	})
	if err != nil {
		return Extraction{}, mapErr("save extraction", err)
	}
	var stored ir.Doc
	if err := json.Unmarshal(row.IR, &stored); err != nil {
		return Extraction{}, fmt.Errorf("extraction %s: %w", row.ID, err)
	}
	return Extraction{ID: row.ID, Doc: &stored}, nil
}

func gzipBytes(b []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(b); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// gunzip decompresses b, refusing more than maxBody bytes of output.
func gunzip(b []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	out, err := io.ReadAll(io.LimitReader(zr, maxBody+1))
	if err != nil {
		return nil, err
	}
	if len(out) > maxBody {
		return nil, errors.New("body larger than 5 MiB")
	}
	return out, zr.Close()
}
