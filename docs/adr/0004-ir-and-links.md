# 0004: IR, block IDs, link rules and framework adapters

Accepted, 2026-11-02. Updated 2026-11-03 for framework adapters (P07). IR version 1 frozen 2026-11-04 (P08).

## Context

The HTML extractor (P06), framework adapters (P07) and PDF import (P12) must all produce one document shape. Annotations need block identities that survive re-imports. Links decide what a student can import next. Real documentation sites wrap their content in framework chrome: anchor labels, copy buttons, line numbers, tabs, feedback widgets.

## Decision

- `internal/ir` is a document of blocks from a closed set: heading, paragraph, list, code, table, callout, image, quote, divider, tabs. Blocks hold inline runs with marks (bold, italic, code, kbd, strike, sub, sup) and links, never raw HTML. List and tab items are `{label, children}`, and only tabs have a label. Code may have a title such as a file name, and a `title_link` (`{href, kind}`, same_domain or external) when the title links to that file. `Validate` enforces each kind's fields, absolute http(s) URLs and unique IDs.
- **`ir_version` 1 is frozen.** From P08 on, extractions and users' documents are stored as IR, so the shape of version 1 no longer changes: no field, kind, mark, link kind, variant or warning code is added, renamed or given a new meaning. Any change bumps `ir_version` and comes with a documented upgrade path that converts stored version 1 documents, in the same change.
- A block ID is the first 12 hex chars of sha256(heading path, kind, text). Prose text has its whitespace collapsed; code text is used exactly, with its language and title. Repeats get `-2`, `-3` in document order.
- Links resolve against `<base href>`, else the page's final URL. `#x`, or this page plus a fragment, becomes an anchor `#x`. The same host as the page, ignoring port and one leading `www.`, is `same_domain`. Other http(s) and mailto links are `external`. Other schemes lose the link and keep the text. Hrefs are kept as resolved.
- **Code text is what the page's copy button puts on the clipboard, when the page carries it.** Expressive Code keeps that text in the copy button's `data-code`, with U+007F for each line break. Otherwise the text is the `<pre>`'s text content (with `<br>` as `\n`), minus what is never text:
  - line-number gutters;
  - copy-button labels;
  - prompts drawn as separate, unselectable elements;
  - frame chrome;
  - lines the framework hides and its copy button leaves out (mdBook's boring lines).

  Visible text stays, including prompts written into the code (`>>>`) and diff lines a copy button skips. Copy buttons that rewrite text with patterns, such as stripping `$ `, are not followed.
  One exception keeps what the reader sees: FastAPI's terminal blocks (`div.termy`) hold `<font>` and `<span>` tags as literal text, which their script renders as colours. In those blocks only, the tags are removed and their content kept, and the language is `console`. HTML parsing itself drops one newline right after `<pre>` and turns CR and CRLF into LF.
- **Adapters are configuration in `internal/extract/adapter`:** detection signals, a content root, required elements, chrome to remove, and callout, tab and code rules. The extractor carries out every adapter's plan, so the transforms exist once.
  - Detection runs in phases across all adapters: generator meta tag, then DOM signature, then host. Within a phase, registry order decides. `doc.extractor` names the adapter and its version.
  - An adapter falls back to the generic rules, with an `adapter_fallback` warning, in three cases:
    - its root or a required element is missing;
    - its root leaves out a heading or `<pre>` that the generic rules keep and that no removal accounts for;
    - removed chrome holds a `<pre>`.

    An adapter drops content only through its Remove list.
- Fetches reach private networks only with `AllowPrivateNetworks`, which config permits in dev and test only.

## Consequences

- Adapters and the PDF importer only have to emit valid IR.
- Extractor fixes that keep the IR shape (better text, fewer junk blocks, a new adapter) need no version bump; stored documents keep the IR they were made with.
- Editing a block changes its own ID and its containers' IDs. Renaming a heading changes the IDs below it, so annotations will need re-anchoring.
- Subdomains (docs.x.com vs api.x.com) count as external.
- A site redesign that breaks an adapter's selectors shows up as an `adapter_fallback` warning, not as lost content. `make test-live` checks real pages for drift.
- A new adapter needs at least two golden fixtures and its own rules in `TestCodeIsVerbatim`'s independent walker.
- Extractor changes regenerate `testdata/golden` with `-update` and a reviewed diff. `TestCodeIsVerbatim` keeps code exact and `TestGoldenHasNoJunk` keeps chrome out.
