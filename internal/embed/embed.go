// Package embed turns text into vectors for search
// (docs/adr/0011-retrieval.md): an OpenAI-compatible HTTP client, Ollama in
// dev, and a deterministic fake for tests.
package embed

import (
	"context"
	"errors"
)

// Dims is the length of every vector: Qwen3-Embedding-0.6B's, and what the
// chunks table stores.
const Dims = 1024

// ErrRejected means the embedder refused the request itself (a 4xx):
// sending it again won't help.
var ErrRejected = errors.New("embedder rejected the request")

// Embedder turns texts into unit vectors of Dims, one per text, in order.
type Embedder interface {
	// Model names the model, so vectors from two models are never compared.
	Model() string
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}
