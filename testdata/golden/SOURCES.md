# Golden fixture sources

Each fixture is a directory with `input.html`, `meta.json` (URLs and a fixed fetch time) and `expected.json` (the extractor's output). Regenerate the outputs with `go test ./internal/extract -update` and review the diff.

## Hand-written

These contain no third-party content: base-href-links, br-in-pre, callouts, chrome-removal, crlf, entities, expressive-code, go-tabs, leading-empty-line, long-line, marks, nested-highlight, nested-lists, never-text-in-code, no-main, table, trailing-whitespace, unicode.

Synthetic framework pages reproduce the DOM structure of a real page, with invented text, where the real page's license does not allow a copy:

| Fixture | Structure of |
|---|---|
| starlight-rag | https://myengineeringpath.dev/programming/python/rag-pipeline-tutorial/ (Starlight) |

## Excerpts of real pages

Trimmed copies of public documentation pages:
- The head was reduced to charset, title and canonical link, plus the generator tags an adapter detects.
- The site chrome was replaced by a minimal header, nav and footer.
- The content was cut at a section boundary, with the elements still open at the cut closed.
- Text and code inside the kept sections are unchanged.

| Fixture | Source | Kept | License | Retrieved |
|---|---|---|---|---|
| real-go-effective-go | https://go.dev/doc/effective_go, "Effective Go" by The Go Authors | Introduction to Commentary | Text: Creative Commons Attribution 4.0. Code: BSD license. Per https://go.dev/copyright | 2026-11-02 |
| real-python-controlflow | https://docs.python.org/3/tutorial/controlflow.html, © Python Software Foundation | Sections 4.1 and 4.2 | Text: Python Software Foundation License Version 2. Code: Zero Clause BSD License | 2026-11-02 |
| real-kubernetes-pods | https://kubernetes.io/docs/concepts/workloads/pods/, "Pods" by The Kubernetes Authors | "What is a Pod?" and "Using Pods" | Creative Commons Attribution 4.0 | 2026-11-02 |
| starlight-tabs | https://starlight.astro.build/components/tabs/, "Tabs" by the Starlight contributors | Introduction to the first Usage example | MIT, per https://github.com/withastro/starlight/blob/main/LICENSE | 2026-11-03 |
