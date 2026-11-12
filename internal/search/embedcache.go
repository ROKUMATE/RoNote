package search

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/rokumate/ronote/internal/embed"
	"github.com/rokumate/ronote/internal/store"
)

// Embedded is a set of texts' vectors and what they cost.
type Embedded struct {
	Vectors [][]float32
	// Hits counts texts the cache had; Sent counts those sent to the
	// embedder, each once.
	Hits, Sent int
}

// EmbedCached embeds texts through st's cache, shared across users: only
// texts it lacks go to e, and what comes back is kept for next time.
func EmbedCached(ctx context.Context, st *store.Store, e embed.Embedder, texts []string) (Embedded, error) {
	hashes := make([][32]byte, len(texts))
	keys := make([][]byte, 0, len(texts))
	seen := map[[32]byte]bool{}
	for i, t := range texts {
		hashes[i] = sha256.Sum256([]byte(t))
		if !seen[hashes[i]] {
			seen[hashes[i]] = true
			keys = append(keys, hashes[i][:])
		}
	}
	found, err := st.CachedEmbeddings(ctx, e.Model(), keys)
	if err != nil {
		return Embedded{}, err
	}
	out := Embedded{Vectors: make([][]float32, len(texts))}
	var missing []string
	var missingHashes [][32]byte
	queued := map[[32]byte]bool{}
	for i, h := range hashes {
		switch {
		case found[h] != nil:
			out.Hits++
		case !queued[h]:
			queued[h] = true
			missing = append(missing, texts[i])
			missingHashes = append(missingHashes, h)
		}
	}
	if len(missing) > 0 {
		vecs, err := e.Embed(ctx, missing)
		if err != nil {
			return Embedded{}, err
		}
		if len(vecs) != len(missing) {
			return Embedded{}, fmt.Errorf("embedder gave %d vectors for %d texts", len(vecs), len(missing))
		}
		for i, v := range vecs {
			if err := st.CacheEmbedding(ctx, e.Model(), missingHashes[i][:], v); err != nil {
				return Embedded{}, err
			}
			found[missingHashes[i]] = v
		}
		out.Sent = len(missing)
	}
	for i, h := range hashes {
		out.Vectors[i] = found[h]
	}
	return out, nil
}
