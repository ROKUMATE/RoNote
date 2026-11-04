# 0006: Editor and document content format

Accepted, 2026-11-04

## Context

From P10 on, users read and edit their documents. An edit must never pass as source:
- code, tables and tabs from the page stay exactly as extracted;
- an edited source block can always be put back;
- notes the user adds are theirs.

The extraction IR is frozen at version 1 (ADR 0004), so the edit history cannot live in it.

## Decision

- **Editor: Tiptap, MIT core only, headless. No Tiptap Pro extensions.** BlockNote was the alternative. It was rejected for three reasons:
  - its block model wants every block editable and movable, and our locked source blocks and atomic nodes fight it;
  - its UI kits impose a look DESIGN.md does not allow;
  - it adds size on top of Tiptap and ProseMirror.
- **Document content format, DocContent v1:** `{doc_version: 1, blocks}`.
  - Each top-level block is an IR block plus three fields:
    - `origin`: `source` or `user`;
    - `edited`: source blocks only;
    - `source_block_id`: source blocks only, naming the block of the document's extraction it came from.
  - Origin and edits are tracked at the top level only: a list, callout, table or tabs block is one unit.
  - User blocks get fresh UUIDv7 ids.
  - Migration 00005 converts stored IR documents, every block `origin: source`, `edited: false`. Its down migration rebuilds the IR from the extraction's metadata and the current blocks.
  - The document's title stays in `documents.title`. Source, extractor and warnings stay in its extraction; the API returns the warnings with the document.
- **Rules the server enforces on every save,** `PATCH /api/v1/documents/{id}` with `{content, version}`:
  - **Concurrency:** the version must be the current one, else 409 `version_conflict` with `current_version`. A save never overwrites a newer version.
  - **Known sources:** every source block names a block of the document's extraction, at most once, else 422 `unknown_source_block`.
  - **Locked blocks:** every code, table, tabs and image block inside a source block, at any depth, equals the extraction's block with the same id, byte for byte, else 422 `source_block_modified`. In v1 they are atomic in the editor and read-only.
  - **Honest flags:** a source block with `edited: false` equals its extraction block, else 422 `source_block_modified`. So an edit cannot hide as source.
  - **User kinds:** user blocks, nested children included, use only paragraph, heading, list, quote, callout and divider, else 422 `block_kind_not_allowed`.
  - **Shape:** anything else invalid answers 422 `invalid_content`.
  - Deleting a source block is allowed; reordering is not offered in v1.
- **Revert:** `POST /api/v1/documents/{id}/blocks/{blockId}/revert` puts the extraction's block back, byte for byte, clears `edited` and bumps the version.
- **In the editor:**
  - Unedited source blocks are saved exactly as loaded, never re-serialized, so Tiptap's normalizing can never touch them.
  - A plugin marks a touched source block `edited`. When a split duplicates an id, the new half becomes a user block.
  - The IR ⇄ Tiptap mapping lives in one module, and every golden fixture must round-trip through it unchanged.
- **Tabs use Radix Tabs**, the first Radix dependency. The platform has no tabs element, and the WAI-ARIA tabs pattern (roving focus, arrow keys, panels) is what Radix Primitives give without imposing a look.
- **Syntax highlighting: Shiki, fine-grained.**
  - `shiki/core` with the JavaScript regex engine, so there is no WebAssembly.
  - Each language loads as its own chunk on demand, and a block is highlighted only when it nears the viewport.
  - The theme maps to six token colours, light and dark.
  - Highlighting only wraps the verbatim text in spans; a test checks the highlighted text equals the block text.
- **Budgets,** gzip, checked per chunk by `make web-build`:
  - initial JS at most 150 kB;
  - the document route chunk has its own budget (CONTEXT.md records the number and why);
  - the highlighter core is a chunk of its own;
  - Shiki's language chunks are listed, not budgeted.

## Consequences

- Edits are visible: an edited source block shows a rule and an "edited" label, and can be reverted.
- Code, tables and tabs cannot be changed in v1, by the editor or by forging an API call.
- A deleted source block cannot be restored except by undo in the same session.
- Syncing a document to a newer snapshot (re-import) will have to merge user blocks and edits; DocContent keeps what that needs.
