package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"

	"github.com/rokumate/ronote/internal/store/sqlcdb"
)

// NewChunk is a piece of a document as search reads it
// (docs/adr/0011-retrieval.md).
type NewChunk struct {
	// BlockIDs are its top-level blocks, in order.
	BlockIDs    []string
	HeadingPath []string
	// Kind is prose, code or note.
	Kind string
	Text string
	Lang string
	// Headings, Prose and Code are the text full-text search reads in each
	// configuration; Idents is a tsvector literal of identifier tokens.
	Headings, Prose, Code, Idents string
	// Embedding is nil until there is one, of Model.
	Embedding []float32
	Model     string
}

// ReplaceChunks makes chunks a document's whole set, in order. Run it in a
// transaction, so search never sees half a set.
func (s *Store) ReplaceChunks(ctx context.Context, owner, document uuid.UUID, chunks []NewChunk) error {
	if err := s.q.DeleteDocumentChunks(ctx, sqlcdb.DeleteDocumentChunksParams{OwnerID: owner, DocumentID: document}); err != nil {
		return mapErr("delete chunks", err)
	}
	for i, c := range chunks {
		var embedding *pgvector.HalfVector
		if c.Embedding != nil {
			v := pgvector.NewHalfVector(c.Embedding)
			embedding = &v
		}
		err := s.q.InsertChunk(ctx, sqlcdb.InsertChunkParams{
			OwnerID: owner, DocumentID: document, BlockIDs: c.BlockIDs, HeadingPath: c.HeadingPath,
			Kind: c.Kind, Text: c.Text, Lang: c.Lang, Position: int32(min(i, 1<<30)), //nolint:gosec // clamped
			Headings: c.Headings, Prose: c.Prose, Code: c.Code, Idents: c.Idents,
			Embedding: embedding, Model: c.Model,
		})
		if err != nil {
			return mapErr("insert chunk", err)
		}
	}
	return nil
}

// CountChunks counts a document's chunks.
func (s *Store) CountChunks(ctx context.Context, owner, document uuid.UUID) (int, error) {
	n, err := s.q.CountDocumentChunks(ctx, sqlcdb.CountDocumentChunksParams{OwnerID: owner, DocumentID: document})
	if err != nil {
		return 0, mapErr("count chunks", err)
	}
	return int(n), nil
}

// CachedEmbeddings returns the cached embeddings by model of the texts
// whose SHA-256s are hashes, by hash.
func (s *Store) CachedEmbeddings(ctx context.Context, model string, hashes [][]byte) (map[[32]byte][]float32, error) {
	rows, err := s.q.GetCachedEmbeddings(ctx, sqlcdb.GetCachedEmbeddingsParams{Model: model, Hashes: hashes})
	if err != nil {
		return nil, mapErr("cached embeddings", err)
	}
	found := make(map[[32]byte][]float32, len(rows))
	for _, r := range rows {
		found[[32]byte(r.TextSha256)] = r.Embedding.Slice()
	}
	return found, nil
}

// CacheEmbedding keeps embedding as model's for the text whose SHA-256 is
// hash. One already kept stays.
func (s *Store) CacheEmbedding(ctx context.Context, model string, hash []byte, embedding []float32) error {
	err := s.q.PutCachedEmbedding(ctx, sqlcdb.PutCachedEmbeddingParams{
		Model: model, TextSha256: hash, Embedding: pgvector.NewHalfVector(embedding),
	})
	if err != nil {
		return mapErr("cache embedding", err)
	}
	return nil
}

// SearchQuery is one search of one user's chunks.
type SearchQuery struct {
	Owner uuid.UUID
	// Document limits the search to one document; uuid.Nil means all.
	Document uuid.UUID
	// Text is the query; IdentQuery its identifiers as a tsquery, or "".
	Text, IdentQuery string
	// Words searches by words; a Vector, by meaning, among chunks Model
	// embedded. Either may be left out.
	Words  bool
	Vector []float32
	Model  string
	// K is reciprocal rank fusion's constant.
	K, Limit int
}

// SearchHit is a chunk a search found, with what it scored.
type SearchHit = sqlcdb.SearchChunksRow

// SearchChunks runs q against q.Owner's chunks alone.
func (s *Store) SearchChunks(ctx context.Context, q SearchQuery) ([]SearchHit, error) {
	var vector *pgvector.HalfVector
	if q.Vector != nil {
		v := pgvector.NewHalfVector(q.Vector)
		vector = &v
	}
	hits, err := s.q.SearchChunks(ctx, sqlcdb.SearchChunksParams{
		OwnerID: q.Owner, Q: q.Text, IdentQuery: q.IdentQuery, UseWords: q.Words,
		DocumentID:  uuid.NullUUID{UUID: q.Document, Valid: q.Document != uuid.Nil},
		QueryVector: vector, Model: q.Model,
		K: int32(min(max(q.K, 1), 1000)), MaxResults: int32(min(max(q.Limit, 1), 1000)), //nolint:gosec // clamped
	})
	if err != nil {
		return nil, mapErr("search chunks", err)
	}
	return hits, nil
}
