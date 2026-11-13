package main

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/rokumate/ronote/internal/search"
	"github.com/rokumate/ronote/internal/store"
)

// block is a top-level block of the corpus, where a reviewer finds it.
type block struct {
	doc, title string
	path       []string
	text       string
}

// review prints each query marked for review: its expected blocks, and
// hybrid's top 5 with the expected ones starred, so a reader can judge
// whether expect names the right blocks. Each names its corpus document,
// since an excerpt and its whole page share a title.
func review(ctx context.Context, st *store.Store, s *search.Searcher, owner uuid.UUID, docs []document, ids []uuid.UUID, queries []query) error {
	// An excerpt shares block ids with its whole page; the first document
	// in the corpus to hold a block names it.
	blocks := map[string]block{}
	// chunks maps each document's chunks by their first block, as hits
	// name them.
	chunks := map[uuid.UUID]map[string]search.Chunk{}
	names := map[uuid.UUID]string{}
	for i, id := range ids {
		names[id] = docs[i].name()
		state, err := st.GetIndexState(ctx, owner, id)
		if err != nil {
			return err
		}
		texts := map[string]string{}
		for j := range state.Content.Blocks {
			b := &state.Content.Blocks[j].Block
			texts[b.ID] = search.BlockText(b)
		}
		chunks[id] = map[string]search.Chunk{}
		for _, c := range search.Chunks(state.Title, state.Content, nil) {
			chunks[id][c.BlockIDs[0]] = c
			for _, b := range c.BlockIDs {
				if _, ok := blocks[b]; !ok {
					blocks[b] = block{doc: names[id], title: state.Title, path: c.HeadingPath, text: texts[b]}
				}
			}
		}
	}

	n := 0
	for _, q := range queries {
		if !q.Review {
			continue
		}
		n++
		fmt.Printf("%s  %s  %q\n", q.ID, q.Category, q.Query)
		fmt.Println("  Expected:")
		for _, id := range q.Expect {
			b, ok := blocks[id]
			if !ok {
				fmt.Printf("    %s  in no chunk\n", id)
				continue
			}
			fmt.Printf("    %s  %s  (%s)\n      %s\n", id, breadcrumb(b.title, b.path), b.doc, preview(b.text, 200))
		}
		res, err := s.Search(ctx, search.Request{Owner: owner, Query: q.Query, Limit: 5, Mode: search.Hybrid})
		if err != nil {
			return fmt.Errorf("%s: %w", q.ID, err)
		}
		fmt.Println("  Hybrid's top 5 (* expected):")
		for i, h := range res.Hits {
			mark := " "
			if slices.ContainsFunc(h.BlockIDs, func(b string) bool { return slices.Contains(q.Expect, b) }) {
				mark = "*"
			}
			fmt.Printf("    %d %s %s  (%s)\n        %s\n", i+1, mark, breadcrumb(h.Title, h.HeadingPath), names[h.DocumentID], preview(chunks[h.DocumentID][h.BlockIDs[0]].Text, 120))
		}
		fmt.Println()
	}
	fmt.Printf("%d queries marked for review\n", n)
	return nil
}

func breadcrumb(title string, path []string) string {
	return strings.Join(append([]string{title}, path...), " > ")
}

// preview is text's first n characters, its whitespace folded to spaces.
func preview(text string, n int) string {
	text = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(text) <= n {
		return text
	}
	return string([]rune(text)[:n]) + "…"
}
