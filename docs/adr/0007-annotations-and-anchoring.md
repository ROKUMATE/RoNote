# 0007: Annotations and anchoring

Accepted, 2026-11-05

## Context

From P12 on, users highlight passages of their documents and attach notes. A highlight has to stay on the same words while the user edits the text around it, inside it, or reverts the block to source. When the words are gone, the highlight and its note must still not be lost.

DocContent (ADR 0006) holds the text. Highlights could live in it as marks, but then every highlight would be an edit, and a source block with a highlight would no longer equal its extraction block.

## Decision

- **Annotations live outside DocContent,** in their own table, one row per highlight:
  - fields: `block_id`, `kind` (`highlight`, the one kind so far), `selector`, an optional `note` of at most 5000 characters, and a `version` for optimistic concurrency;
  - owner-scoped like documents: every query takes `owner_id`, and a composite key ties each row to a document of the same owner;
  - at most 2000 per document;
  - saving content never reads or changes them, and the editor shows them as decorations, never marks. So DocContent stays as ADR 0006 defines it, and a highlight is not an edit.
- **A selector names a range in one block's plain text,** in the style of the W3C Web Annotation model:
  - `{position: {start, end}, quote: {exact, prefix, suffix}}`;
  - `prefix` and `suffix` are at most 32 characters of the text before and after;
  - **plain text** is the joined text of a heading's or paragraph's runs, or a code block's stored `text`;
  - **offsets** count UTF-16 code units, as the browser does. The server converts, and one fixture (`testdata/anchoring`) keeps the Go and TypeScript readings of block text equal.
- **Where highlights go:**
  - headings and paragraphs at any depth the editor edits: top level, lists, quotes and callouts;
  - code blocks at any depth, tabs included.

  Table cells, image captions, and prose inside tabs are drawn read-only and take no highlights in v1.
- **On every write, the server checks the selector against the stored content:**
  - the block exists: else 422 `unknown_block`;
  - the offsets fit its text, and `exact` equals the text there: else 422 `quote_mismatch`.

  `prefix` and `suffix` are hints, so they are not checked.
- **Re-anchoring** finds a selector's range in a block's current text. It is one module of pure functions, tried in order:
  1. **Position:** the text at the offsets still equals `exact`.
  2. **Exact quote:** every occurrence of `exact` in the block. Duplicates are told apart by how many characters of `prefix` and `suffix` agree, then by distance from the old position.
  3. **Fuzzy:** the substring of the block closest to `exact` by edit distance (Sellers' algorithm, linear in the block's length for a given quote). It is accepted when the similarity `1 − distance / length(exact)` is at least **0.75**. Ties go to context, then position.
  4. **Orphaned:** none of these found it.
- **Why 0.75:**
  - an edited word in a quote of five or more words changes at most about a fifth of its characters, so it stays placed;
  - replacing half the words, or editing a one- or two-word quote, falls below and orphans it.

  Matching less closely than that more often finds a different passage than the same one, edited. The anchoring tests pin cases just above and just below.
- **Orphans are derived, never stored or deleted.** An annotation is orphaned exactly when it can't be anchored in the current content: its block was deleted, or its words changed past the threshold.
  - Its stored selector and note stay as they were.
  - The document lists it as an unplaced highlight. From there the user removes it, or re-applies it by selecting new text, which saves a new `block_id` and selector.
- **When re-anchoring is saved:**
  - after each successful content save, and after a revert, the annotations in blocks whose text changed are re-anchored against the content just stored;
  - their changed selectors go in one batch (`POST /documents/{id}/annotations/reanchor`), debounced by a second;
  - anchoring against the stored content, not the editor's newer text, means the server's quote check sees the same text;
  - the batch is a separate request and never holds up the content save;
  - between saves, the editor re-anchors the edited block on every change, so a highlight follows typing.
- **Conflicts:** each annotation has a version. A stale `PATCH` answers 409 `version_conflict` with `current_version`. A reanchor batch is all or nothing: one stale version fails the whole batch with 409, and the client reloads the annotations and re-anchors again.
- **One look:** highlights use `--color-mark` in both themes, the colour of the reader's existing `mark`, so there are no new colours. A user's note sits in the right margin when the screen is wide enough, else under its highlight.
- **Size:** the anchoring code and the editor plugin that draws highlights ship with the document route. The toolbar, popover, margin notes and highlights list load as their own chunk once the editor is up, with their own budget in `apps/web/scripts/budget.ts`.

## Consequences

- Editing text never loses a highlight. At worst it becomes unplaced, with its quote and note, until the user removes or re-applies it.
- A source block with highlights still equals its extraction block, so ADR 0006's rules and Revert are unchanged.
- A highlight whose block text changed in another tab is re-anchored by whichever tab saves; a stale batch reloads and retries.
- Highlighting inside tables, captions and tab prose would need those blocks to be editable or to carry their own selection handling.
