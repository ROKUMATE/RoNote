# 0011: Retrieval

Accepted, 2026-11-12

## Context

From P16 on, students search their whole library, and from P17 they chat over it. Both need retrieval that finds a passage by its words, such as an identifier or an error message, and by its meaning, such as a question in a student's own words. Answers cite blocks of the student's documents. The verbatim rule (ADR 0002) is unchanged: nothing generated ever enters DocContent (ADR 0006); retrieval reads documents and notes, and writes only its own tables.

## Decision

- **Chunks.** A chunk is consecutive top-level blocks under the same heading path, up to about 350 tokens.
  - Tokens are estimated from characters: 4 characters a token, so 1,400 characters. English prose runs close to that with Qwen3's tokenizer; dense code measured 2.65.
  - A heading starts a new chunk, and its text is part of the path, not the chunk.
  - A code block is never split. One over the limit is a chunk of its own: its stored text stays whole, and only its embedding input is cut, at 6,000 characters. Dense code that long measured 2,262 tokens, well inside the 4,096 Ollama embeds; past that it truncates without saying so.
  - Each chunk's embedding input starts with its breadcrumb, "Doc title > H2 > H3", and labels code with its language, or "code" when the page gives none.
  - A highlight with a note is a chunk of its own, kind `note`, linked to its block: its input is the breadcrumb, the highlighted words and the note.
  - Lists, callouts, quotes, tables and tabs are read as text, nested code included; a tabs block's code stays whole.
- **Embeddings.** One `Embedder` interface at the package boundary, with three implementations:
  - an OpenAI-compatible HTTP client (`/v1/embeddings`), Ollama in dev now, TEI or a hosted service later by config (`RONOTE_EMBED_URL`, `RONOTE_EMBED_MODEL`);
  - a deterministic fake for unit tests, and `cmd/fakeembed`, the same fake served over HTTP for the end-to-end tests, like `cmd/fakeidp`, dev and test only;
  - a replay embedder, which reads vectors recorded for the eval and fails on any text it has none for.
- **The model: Qwen3-Embedding-0.6B,** Ollama tag `qwen3-embedding:0.6b`, pinned to digest `ac6da0dfba84` (Q8_0, 1024 dimensions, unit-normalised, 32k context). `make doctor` checks it is pulled.
  - Queries get Qwen3's instruction prefix, `Instruct: Given a web search query, retrieve relevant passages that answer the query\nQuery: `; documents don't.
  - Vectors are stored as `halfvec(1024)`, half the size of `vector`; the eval's recorded vectors are half precision too, so it measures what search stores.
  - A 4xx from the embedder is permanent; anything else is retried with the jobs port's backoff.
- **The cache,** `embedding_cache(model, text_sha256) → halfvec`, is shared across users, since the same page in two libraries embeds the same text. It is internal: no API reads it. Changing `RONOTE_EMBED_MODEL` reindexes every document, and vectors from two models are never compared: every vector carries its model, and search reads only the configured one's.
- **Vector search is an exact scan** of the user's own chunks, through an index that starts with `owner_id`, ordered by cosine distance. There is no ANN index. A library holds hundreds to a few thousand chunks, and an exact scan is exact: HNSW would trade recall for speed it doesn't need there, and its filtered searches miss rows unless pgvector's iterative scans are on. The threshold at which HNSW with iterative scans becomes worth it is measured below.
- **Words:** Postgres full-text search, in three parts of one `tsvector`:
  - prose with the `english` configuration, headings weighted A and text B;
  - code with the `simple` configuration, weight C;
  - an identifier field, weight A: tokens from code and inline code split on punctuation and camelCase, and kept whole too. `http.ServeMux` gives `http.servemux`, `http`, `servemux`, `serve` and `mux`. A query is split the same way, so `ServeMux`, `http.ServeMux`, `errgroup.WithContext`, `ERR_MODULE_NOT_FOUND`, `__init__`, `--no-verify` and `useEffect` each find their chunk.
- **Hybrid:** the words half and the vector half each take the user's top 50, and reciprocal rank fusion merges them: `score = Σ 1/(k + rank)`, with k = 60. RRF needs no calibration between a text rank and a cosine distance, which have no common scale; k = 60 is the value from Cormack, Clarke and Büttcher (2009), and on the eval 10, 30 and 60 differ by at most 0.001 in any metric.
  - Beside meaning, every English term must match: there the words half is for precision. Searching by words alone (degraded mode), the terms are OR'ed, so a question in a student's own words still finds chunks with some of them. On the eval, AND'ing beside meaning scores hybrid MRR@10 0.794 against 0.743 OR'ed; OR'ing alone lifts words-only recall@10 from 0.40 to 0.81.
- **Degraded mode:** when the embedder doesn't answer within 2 seconds, search returns word matches alone, and says so (`degraded: true`).
- **Indexing runs on the jobs port.**
  - `documents` gains `index_seq`, bumped in the transaction of every change search sees (an import finishing, a save, a revert, a note created, changed or deleted), and `indexed_seq`. The same transaction enqueues `index_document`, keyed by document and sequence and delayed 5 seconds, so a burst of edits collapses into one run.
  - The handler indexes the document's state at the time it runs; a job whose sequence is behind does nothing.
  - Re-chunking embeds only text the cache lacks, so unchanged chunks cost nothing.
  - The worker backfills on start: documents never indexed, behind, or indexed with another model, five a second.
- **The search API:** `GET /api/v1/search?q=&document_id=&limit=`, owner-scoped and counted in the per-user rate limit; `q` at most 500 characters, `limit` at most 50. Each result names its document, block, heading path and kind, and carries a snippet as text segments with the matched words marked (`ts_headline` for word hits, the chunk's start for meaning-only hits). Scores are returned in dev and test only.

## Measured

On this Mac (Apple silicon, Postgres 18 and pgvector 0.8.7 in a podman VM), `make test-perf-search` and `make eval`, 2026-11-12:

- **Exact scan, one user among 200,000 other users' chunks:**

  | User's chunks | Meaning half, fastest | Hybrid p50 | Hybrid p95 |
  |---|---|---|---|
  | 5,000 | 2.4 ms | 9.3 ms | 11.5 ms |
  | 10,000 | 4.8 ms | 13.6 ms | 16.4 ms |
  | 20,000 | 8.2 ms | 11.0 ms | 17.4 ms |
  | 25,000 | 10.3 ms p50 | 13.2 ms | 21.1 ms |

  The meaning half grows linearly, about 0.4 µs a chunk once the user's rows are cached; read from disk, EXPLAIN ANALYZE measured 37 ms at 20,000 (1.9 µs a chunk). Hybrid adds a fixed few milliseconds: snippets and the words half.
- **The embedding column's storage:** pgvector stores `halfvec` EXTERNAL, out of line once a row passes 2 kB, as a 1024-dimension vector with its chunk's text always does. At 25,000 chunks PLAIN searched by meaning in 10.3 ms p50 (EXTERNAL 31.1 ms) and hybrid in 13.2 ms p50, 21.1 ms p95 (EXTERNAL 39.2 and 52.7). Migration 00011 makes it PLAIN.
- **When HNSW becomes worth it:** when one user's chunks pass about 25,000, a search from disk passes 50 ms; past about 100,000 even a cached one does. Then an HNSW index with pgvector's iterative scans (`hnsw.iterative_scan = relaxed_order`), so filtering by owner doesn't lose rows, is worth its recall cost. The eval's pages average 16 chunks, so the 500-document quota keeps a typical library near 8,000; a library of Effective Go-sized pages (102 chunks) could reach 51,000.
- **The eval:** 24 documents (10 whole pages, 14 excerpts), 381 chunks, 80 queries. Hybrid: recall@5 0.880, recall@10 0.944, MRR@10 0.794, nDCG@10 0.819; by meaning alone 0.887, 0.938, 0.783, 0.807; by words alone 0.688, 0.808, 0.520, 0.578. `testdata/retrieval/baseline.json` holds hybrid's per category.
- **No answer:** the best cosine similarity of queries with no answer ran 0.28 to 0.60 (median 0.37); of answerable ones 0.29 to 0.78 (median 0.61). The two overlap, so P17 can't refuse on similarity alone. The fused score can't tell them apart at all: a top result scores 1/61 whatever it is.

## Consequences

- A library's search is its own: every query on `chunks` names `owner_id`, as for documents (`TestOwnedQueriesAreScoped`).
- Edits show up in search a few seconds after they are saved.
- With the embedder down, search still finds words, and says that's all it found.
- Recall is measured, not assumed: `make eval` replays recorded vectors against a fixed corpus and fails CI when hybrid search gets worse.
