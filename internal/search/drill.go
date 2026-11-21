package search

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/rokumate/ronote/internal/embed"
	"github.com/rokumate/ronote/internal/store"
)

// CheckSearch is the restore drill's search (docs/adr/0015-operations.md):
// the newest chunk's owner's library, searched by words alone for a word
// that chunk is indexed by, which a restored index must find. It returns
// the number of hits, never what they are; searched is false when nothing
// is indexed.
func CheckSearch(ctx context.Context, st *store.Store) (hits int, searched bool, err error) {
	owner, word, found, err := st.ChunkToSearch(ctx)
	if err != nil || !found {
		return 0, false, err
	}
	// Words alone ask no embedder, which the drill's scratch database has none of.
	s := NewSearcher(st, embed.Fake{}, slog.New(slog.DiscardHandler))
	res, err := s.Search(ctx, Request{Owner: owner, Query: word, Limit: 5, Mode: WordsOnly})
	if err != nil {
		return 0, true, fmt.Errorf("search the restored library: %w", err)
	}
	if len(res.Hits) == 0 {
		return 0, true, fmt.Errorf("search found nothing in the restored library for a word of its newest chunk")
	}
	return len(res.Hits), true, nil
}
