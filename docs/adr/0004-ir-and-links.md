# 0004: IR, block IDs and link rules

Accepted, 2026-11-02

## Context

The HTML extractor (P06), framework adapters (P07) and PDF import (P12) must all produce one document shape. Annotations need block identities that survive re-imports. Links decide what a student can import next.

## Decision

- `internal/ir` is a document of blocks from a closed set: heading, paragraph, list, code, table, callout, image, quote, divider. Blocks hold inline runs with marks (bold, italic, code, kbd, strike, sub, sup) and links, and never raw HTML. `Validate` enforces each kind's fields, absolute http(s) URLs and unique IDs. `ir_version` is 1.
- A block ID is the first 12 hex chars of sha256(heading path, kind, text). Prose text has its whitespace collapsed; code text is used exactly. Repeats get `-2`, `-3` in document order.
- Links resolve against `<base href>`, else the page's final URL. `#x`, or this page plus a fragment, becomes an anchor `#x`. The same host as the page, ignoring port and one leading `www.`, is `same_domain`. Other http(s) and mailto links are `external`. Other schemes lose the link and keep the text. Hrefs are kept as resolved.
- A code block's text is the exact text content of its `<pre>`, with `<br>` as `\n`. Only HTML parsing changes it: one newline right after `<pre>` is dropped, and CR and CRLF become LF.
- Fetches reach private networks only with `AllowPrivateNetworks`, which config permits in dev and test only.

## Consequences

- Adapters and the PDF importer only have to emit valid IR.
- Editing a block changes its own ID and its containers' IDs. Renaming a heading changes the IDs below it, so annotations will need re-anchoring.
- Subdomains (docs.x.com vs api.x.com) count as external.
- Extractor changes regenerate `testdata/golden` with `-update` and a reviewed diff. `TestCodeIsVerbatim` keeps code exact.
