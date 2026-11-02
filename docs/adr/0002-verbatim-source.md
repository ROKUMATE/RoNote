# 0002: Verbatim source

Accepted, 2026-11-02

## Context

Students study from documentation they need to trust. Rewritten prose drifts from the source, and one changed character in a code block can break the code or change what it teaches. When a tool summarizes or cleans up content, those changes are hard to spot.

## Decision

- Source text is never rewritten. Extraction may change structure (blocks, headings, links) but not the words.
- Code blocks stay byte-identical to the source, whitespace included.
- No AI-written content is added to a v1 doc.
- Later machine-made content (OCR fixes, converted code) is stored apart from the source. It is shown with a thin left rule and a "generated" label (DESIGN.md).
- TestCodeIsVerbatim (P06) checks code blocks against `testdata/golden/`.

## Consequences

- Parsers keep whitespace as found. No normalizing pass runs over text or code.
- Bad source stays bad: typos and broken examples are reproduced, not fixed.
- Every importer change comes with golden cases and keeps TestCodeIsVerbatim green.
