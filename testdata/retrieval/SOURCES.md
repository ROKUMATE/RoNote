# Retrieval eval sources

The eval (`cmd/eval`, `make eval`, docs/adr/0011-retrieval.md) searches a corpus of real documentation: the extractor's IR of ten whole pages, in `corpus/`, and the golden fixtures cut from real pages (`testdata/golden/SOURCES.md` has those excerpts' sources and licences). `corpus.txt` lists both. An excerpt and its whole page are separate documents, as two imports of the same site's pages would be, and share block ids, so an expected block found in either counts.

No synthetic pages are used: the real ones cover every category of query.

## Whole pages

Each file is `go run ./cmd/extract --format=json <url>`, fetched on 2026-11-12, unchanged. Every licence allows redistributing the text with attribution.

| File | Source | License |
|---|---|---|
| docusaurus-code-blocks.json | https://docusaurus.io/docs/markdown-features/code-blocks, "Code blocks" by Meta Platforms, Inc. and the Docusaurus contributors | Creative Commons Attribution 4.0, per https://github.com/facebook/docusaurus/blob/main/LICENSE-docs |
| effective-go.json | https://go.dev/doc/effective_go, "Effective Go" by The Go Authors | Text: Creative Commons Attribution 4.0. Code: BSD license. Per https://go.dev/copyright |
| fastapi-first-steps.json | https://fastapi.tiangolo.com/tutorial/first-steps/, "First Steps" by Sebastián Ramírez and the FastAPI contributors | MIT, per https://github.com/fastapi/fastapi/blob/master/LICENSE |
| kubernetes-pods.json | https://kubernetes.io/docs/concepts/workloads/pods/, "Pods" by The Kubernetes Authors | Creative Commons Attribution 4.0 |
| mdn-closures.json | https://developer.mozilla.org/en-US/docs/Web/JavaScript/Guide/Closures, "Closures" by Mozilla Contributors | Text: Creative Commons Attribution-ShareAlike 2.5 or later; this file is shared under the same licence. Code: CC0, per https://developer.mozilla.org/en-US/docs/MDN/Writing_guidelines/Attrib_copyright_license |
| postgres-select.json | https://www.postgresql.org/docs/current/tutorial-select.html, "2.5. Querying a Table", © The PostgreSQL Global Development Group | The PostgreSQL License, per https://www.postgresql.org/docs/current/legalnotice.html |
| python-controlflow.json | https://docs.python.org/3/tutorial/controlflow.html, "4. More Control Flow Tools", © Python Software Foundation | Text: Python Software Foundation License Version 2. Code: Zero Clause BSD License |
| requests-quickstart.json | https://requests.readthedocs.io/en/latest/user/quickstart/, "Quickstart" by Kenneth Reitz and the Requests contributors | Apache License 2.0, per https://github.com/psf/requests/blob/main/LICENSE |
| rust-data-types.json | https://doc.rust-lang.org/book/ch03-02-data-types.html, "Data Types" from The Rust Programming Language by Steve Klabnik, Carol Nichols and Chris Krycho, with contributions from the Rust Community | MIT or Apache License 2.0, at the reader's option, per https://github.com/rust-lang/book/blob/main/COPYRIGHT |
| vite-guide.json | https://vite.dev/guide/, "Getting Started" by the Vite contributors | MIT, per https://github.com/vitejs/vite/blob/main/LICENSE |

## Left out

- https://myengineeringpath.dev/programming/python/rag-pipeline-tutorial/, the eleventh page `make test-live` extracts: it states no licence that allows a copy. Its golden fixture (`starlight-rag`) is synthetic and left out too.
- `material-termy`, a synthetic fixture.

## Queries

`queries.jsonl` holds 80 queries written the way a student asks, not in the pages' words, each with a category and the blocks that answer it:

- `identifier` (15): a name from code, such as `runtime.NumCPU`;
- `error` (10): an error message as a student would paste it;
- `paraphrase` (20): the idea in other words;
- `howto` (17): "how do I…";
- `multidoc` (10): an answer that needs a block from each of two documents; all of `expect` counts;
- `none` (8): no answer in the corpus.

For every category but `multidoc`, any one expected block answers the query. Queries marked `review` are the ones whose expected blocks are most open to judgement.

`vectors-qwen3-embedding-0.6b.bin` holds every chunk's and query's embedding, recorded by `make eval-record`, keyed by the SHA-256 of the text embedded, in half precision.
