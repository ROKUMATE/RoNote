# 0011: Retrieval

Accepted, 2026-11-12

## Context

From P16 on, students search their whole library, and from P17 they chat over it. Both need retrieval that finds a passage by its words, such as an identifier or an error message, and by its meaning, such as a question in a student's own words. Answers cite blocks of the student's documents. The verbatim rule (ADR 0002) is unchanged: nothing generated ever enters DocContent (ADR 0006); retrieval reads documents and notes, and writes only its own tables.

## Decision

- **Chunks.** A chunk is consecutive top-level blocks under the same heading path, up to about 350 tokens.
  - Tokens are estimated from characters: 4 characters a token, so 1,400 characters. English prose runs close to that with Qwen3's tokenizer; code runs denser, about 3.
  - A heading starts a new chunk, and its text is part of the path, not the chunk.
  - A code block is never split. One over the limit is a chunk of its own: its stored text stays whole, and only its embedding input is cut, at 6,000 characters, which keeps dense code under 2,048 tokens.
  - Each chunk's embedding input starts with its breadcrumb, "Doc title > H2 > H3", and labels code with its language, or "code" when the page gives none.
  - A highlight with a note is a chunk of its own, kind `note`, linked to its block: its input is the breadcrumb, the highlighted words and the note.
  - Lists, callouts, quotes, tables and tabs are read as text, nested code included; a tabs block's code stays whole.
- **Embeddings.** One `Embedder` interface at the package boundary, with three implementations:
  - an OpenAI-compatible HTTP client (`/v1/embeddings`), Ollama in dev now, TEI or a hosted service later by config (`RONOTE_EMBED_URL`, `RONOTE_EMBED_MODEL`);
  - a deterministic fake for unit tests, and `cmd/fakeembed`, the same fake served over HTTP for the end-to-end tests, like `cmd/fakeidp`, dev and test only;
  - a replay embedder, which reads vectors recorded for the eval and fails on any text it has none for.
- **The model: Qwen3-Embedding-0.6B,** Ollama tag `qwen3-embedding:0.6b`, pinned to digest `ac6da0dfba84` (Q8_0, 1024 dimensions, unit-normalised, 32k context). `make doctor` checks it is pulled.
  - Queries get Qwen3's instruction prefix, `Instruct: Given a web search query, retrieve relevant passages that answer the query\nQuery: `; documents don't.
  - Vectors are stored as `halfvec(1024)`: half the size of `vector`, and the scores the eval measures don't change in their first three decimals.
  - A 4xx from the embedder is permanent; anything else is retried with the jobs port's backoff.
- **The cache,** `embedding_cache(model, text_sha256) → halfvec`, is shared across users, since the same page in two libraries embeds the same text. It is internal: no API reads it. Changing `RONOTE_EMBED_MODEL` reindexes every document, and vectors from two models are never compared: every vector carries its model, and search reads only the configured one's.
- **Vector search is an exact scan** of the user's own chunks, through an index that starts with `owner_id`, ordered by cosine distance. There is no ANN index. A library holds hundreds to a few thousand chunks, and an exact scan is exact: HNSW would trade recall for speed it doesn't need there, and its filtered searches miss rows unless pgvector's iterative scans are on. The threshold at which HNSW with iterative scans becomes worth it is measured below.
- **Words:** Postgres full-text search, in three parts of one `tsvector`:
  - prose with the `english` configuration, headings weighted A and text B;
  - code with the `simple` configuration, weight C;
  - an identifier field, weight A: tokens from code and inline code split on punctuation and camelCase, and kept whole too. `http.ServeMux` gives `http.servemux`, `http`, `servemux`, `serve` and `mux`. A query is split the same way, so `ServeMux`, `http.ServeMux`, `errgroup.WithContext`, `ERR_MODULE_NOT_FOUND`, `__init__`, `--no-verify` and `useEffect` each find their chunk.
- **Hybrid:** the words half and the vector half each take the user's top 50, and reciprocal rank fusion merges them: `score = Σ 1/(k + rank)`, with k = 60. RRF needs no calibration between a text rank and a cosine distance, which have no common scale; k = 60 is the value from Cormack, Clarke and Büttcher (2009), and the eval checks it against 10 and 30 below.
- **Degraded mode:** when the embedder doesn't answer within 2 seconds, search returns word matches alone, and says so (`degraded: true`).
- **Indexing runs on the jobs port.**
  - `documents` gains `index_seq`, bumped in the transaction of every change search sees (an import finishing, a save, a revert, a note created, changed or deleted), and `indexed_seq`. The same transaction enqueues `index_document`, keyed by document and sequence and delayed 5 seconds, so a burst of edits collapses into one run.
  - The handler indexes the document's state at the time it runs; a job whose sequence is behind does nothing.
  - Re-chunking embeds only text the cache lacks, so unchanged chunks cost nothing.
  - The worker backfills on start: documents never indexed, behind, or indexed with another model, five a second.
- **The search API:** `GET /api/v1/search?q=&document_id=&limit=`, owner-scoped and counted in the per-user rate limit; `q` at most 500 characters, `limit` at most 50. Each result names its document, block, heading path and kind, and carries a snippet as text segments with the matched words marked (`ts_headline` for word hits, the chunk's start for meaning-only hits). Scores are returned in dev and test only.

## Measured

Filled in when P16's measurements are done: search time at 5k, 10k, 20k and 25k chunks, the HNSW threshold, EXTERNAL against PLAIN storage for the embedding column, and the eval's baseline.

## Consequences

- A library's search is its own: every query on `chunks` names `owner_id`, as for documents (`TestOwnedQueriesAreScoped`).
- Edits show up in search a few seconds after they are saved.
- With the embedder down, search still finds words, and says that's all it found.
- Recall is measured, not assumed: `make eval` replays recorded vectors against a fixed corpus and fails CI when hybrid search gets worse.
