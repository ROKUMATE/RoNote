# 0013: Printed PDF import

Accepted, 2026-11-15.

## Context

From P18 on, students import PDFs as well as pages: textbook chapters and lecture slides. A PDF has no DOM to extract from, so its text and layout come from a parser. The verbatim rule still holds (ADR 0002): nothing is added that isn't in the PDF. A PDF is also private in a way a public page is not: a cache shared by everyone would let one user learn that another uploaded a file.

## Decision

- **Scope: digital-born PDFs**, with a text layer. A PDF without one (a scan) fails with `scanned_pdf_unsupported`; OCR is a later phase.
- **Limits.** At most 25 MB and 300 pages, starting with `%PDF-`. A password-protected PDF fails with `pdf_password_protected`. A PDF counts toward the daily import quota like a page (ADR 0010).
- **Parsing: docling-serve,** the CPU image `quay.io/docling-project/docling-serve-cpu:v1.38.2`, pinned by digest (`sha256:7a480a16…`).
  - 1.38.2, not the 1.21.x first named: it was the latest release, five months newer, with the same async API.
  - Async API only: submit (`POST /v1/convert/file/async`, one file), poll (`GET /v1/status/poll/{task_id}`), fetch (`GET /v1/result/{task_id}`), with `X-Api-Key`.
  - Options: `to_formats` `json` only, `do_ocr` false, `include_images` false, `image_export_mode` `placeholder`, `do_pdf_heading_hierarchy` true (so section headers carry levels), `abort_on_error` true. Code enrichment stays off, so code has no language and the language picker applies (ADR 0006).
  - The container has no network egress (an internal network; the image carries its default models), CPU and memory limits, and an API key. `DOCLING_SERVE_MAX_NUM_PAGES` (300), `MAX_FILE_SIZE` (25 MB) and `MAX_DOCUMENT_TIMEOUT` bound each document; results are single use.
  - RoNote never parses PDF bytes itself beyond the magic check. Page count, encryption and missing text are learned from docling's answer.
- **Code comes from the text layer, through pdftext.** Real docling 1.38.2 joins a code block's lines with spaces, drops its indentation and adds spaces before commas, on all three PDF backends and in its Markdown too, so no code from it would run. Its layout model also calls some code prose (a one-line `cd`), finds none in a framed terminal, and draws a code box around a prose label (`Filename:`, `Result:`). The text layer is better: it keeps the line breaks, indentation survives as glyph positions (Chrome prints leading spaces as a gap, not as characters), and each glyph names its font.
  - `deploy/pdftext/pdftext.py` is a small HTTP service run from the docling image, which has pypdfium2, and as sandboxed: no egress, the same limits, its own API key. It takes the PDF and the boxes of docling's code, prose and pictures, and answers each box's lines, each marked monospace or not.
  - A font is monospace when its descriptor sets FixedPitch, or when its glyphs, side by side on a line, all stand whole advances apart (TeX's typewriter fonts don't set the flag). A line is monospace when most of its glyphs are.
  - Lines are grouped by glyph baseline (pdfium's own breaks split a line at each highlighting colour). In a monospace line each glyph goes at the column its origin is in, the column width being the font's advance, and blank lines come from the gaps between baselines; a wide CJK glyph takes two columns. A proportional line gets one space per word gap.
  - Deterministic arithmetic on the text layer, never a model. A code block pdftext finds nothing in keeps docling's text, and the PDF verbatim report counts it.
- **Storage: a blob port** (`internal/blob`), with an S3 adapter (aws-sdk-go-v2, any S3-compatible store) and an in-memory one for unit tests. A PDF's key is `pdf/<owner>/<sha256>`; Postgres keeps its snapshot row with that key instead of the bytes.
  - SeaweedFS runs in dev and CI (`weed server -s3`). Not MinIO: its community images stopped in October 2025, the project went into maintenance mode in December 2025 and was archived in February 2026.
- **Privacy.** A PDF's source, snapshot and extraction belong to one owner and are never shared: a cross-user cache hit would reveal that someone else had uploaded the file. A user who uploads the same PDF twice gets the document they already have. Web pages stay shared (ADR 0005), and so does the embedding cache, which holds vectors of text, not who imported it (ADR 0011).
- **Mapping DoclingDocument JSON to IR,** in reading order (`body`):

  | docling | IR |
  |---|---|
  | `title` | heading, level 1; also the document's title |
  | `section_header` | heading, docling's level |
  | `text`, `paragraph`, `footnote`, `formula`, `reference`, `caption` | paragraph |
  | `list_item` in a list group | list (ordered when the group is) |
  | `table` | table |
  | `code` | code, docling's language if given, else empty |
  | `page_header`, `page_footer` | dropped |
  | `picture` | dropped; its captions kept as paragraphs; a `figures_omitted` warning with the count; a picture of monospace text is code (below) |

  Two rules use pdftext's lines, where docling's layout model errs:
  - a code block's prose lines are paragraphs, and each run of its monospace lines is a code block;
  - prose, or a picture, whose lines are all set in a monospace font is a code block.

  A document set entirely in a monospace font would come out as code; none of the fixtures is. A PDF with no title item is titled by its filename without `.pdf`. Nothing is added that isn't in the PDF.
- **Verbatim for PDFs.** Prose is what docling reads from the text layer; code is what pdftext reads from it. The one transform, in both, expands the Unicode ligatures ﬀ ﬁ ﬂ ﬃ ﬄ (U+FB00–FB04) into their letters. Known losses, which the PDF verbatim report measures:
  - tabs, which come back as the spaces that fill their width, and trailing spaces, which a text layer doesn't keep;
  - a block always ends with a newline, which the page's block may not;
  - an emoji drawn wider than a column may gain a space after it;
  - a line a code block wraps across in print, which comes back as two lines;
  - a code block a page break splits: docling often calls the part on the next page text;
  - in prose, docling's own normalizing: curly quotes made straight, and a space now and then before punctuation.
- **Page anchors: IR version 2.** Every block from a PDF records its 1-based page (`page`, its first page when it spans more). It is a shape change, so ADR 0004's rule applies, by a bump with an upgrade path:
  - IR 2 adds `block.page`, `source.pdf` (`filename`, `pages`; a PDF's source has no URL, and its `body_sha256` is the file's), the `figures_omitted` warning code and a warning's `count`. Nothing in version 1 changes meaning, so a version 1 document is a valid version 2 one;
  - migration 00014 sets every stored extraction's version to 2; the eval corpus and the goldens are rewritten in the same change;
  - DocContent stays `doc_version` 1: `page` is an optional field whose absence means what it meant before (ADR 0006's exception), and the editor carries it through edits like a block's id.
- **API.**
  - `POST /api/v1/imports/pdf`, multipart with one file, answers 202 with the same `Import` shape (its `kind` is `pdf`, with the filename, and no URL). Progress is the same event stream, with a `converting` step between `queued` and `extracting`.
  - `GET /api/v1/documents/{id}/original` streams the PDF to its owner, and 404 to anyone else. Headers: `Content-Type: application/pdf`, `X-Content-Type-Options: nosniff`, `Content-Security-Policy: sandbox; default-src 'none'`, and `Content-Disposition: inline` with the filename made safe (ASCII in `filename`, UTF-8 in `filename*`, no quotes, separators or control characters). Chrome 157 shows a PDF served this way, and `#page=N` lands on page N.
  - `DELETE /api/v1/documents/{id}` deletes a document; a PDF's blobs go with it.
- **Worker.** Kind `import_pdf` on the jobs port (ADR 0009), 2 at a time, 10 minutes an attempt: read the blob, submit it to docling, poll, fetch, read the code boxes' lines from pdftext, map, then the final transaction of ADR 0005 (source, snapshot, extraction, document, `done`). A redelivered job for a finished import does nothing.
- **Errors.**
  - At upload, synchronous: `not_a_pdf` (no `%PDF-`), `pdf_too_large`.
  - Permanent, from docling's answer: `pdf_password_protected`, `pdf_too_many_pages`, `scanned_pdf_unsupported` (pages but no text), `pdf_conversion_failed` (anything else docling refuses), `pdf_conversion_timeout` (docling gave up at its document timeout: the same PDF on the same CPUs would again, and with one conversion worker every other PDF import waits behind each try).
  - Transient: HTTP timeouts, 5xx and network errors, 3 attempts in all; then `pdf_conversion_unavailable`.
  - docling's error wording is matched to these codes. Its recorded answers to hostile fixtures pin the matching, so an upgrade that changes the wording fails tests rather than misreporting.
- **Deletion.** Deleting a PDF document deletes its source, and the blob in a job enqueued in the same transaction; deleting an account enqueues the deletion of `pdf/<owner>/`. A sweep at worker start and daily deletes any blob under `pdf/` that no snapshot or recent import references. Deleting a missing blob succeeds, so every step can run twice.
- **Tests never call docling.** `make pdf-record` records docling's and pdftext's answers for each fixture; `cmd/fakedocling` (dev and test only) replays them over both APIs, keyed by the file's sha256. CI and the end-to-end tests run against it and SeaweedFS, and never pull the docling image.

## Consequences

- A PDF import costs seconds of CPU per page on the docling host, where a page import costs milliseconds; the worker runs 2 at a time.
- PDF blobs live outside Postgres, so a backup needs both stores.
- Two students who upload the same textbook each convert it once.
- Scans, figures and code languages wait for later phases: OCR, picture handling, code enrichment.
