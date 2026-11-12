-- Keep each chunk's embedding in its row. pgvector stores halfvec EXTERNAL,
-- out of line once a row passes 2 kB, which a 1024-dimension vector and its
-- chunk's text always do; every distance then reads a TOAST row too. At
-- 25,000 chunks of one user among 200,000 others, PLAIN searched by meaning
-- in 10 ms at the median against 31 ms for EXTERNAL, hybrid in 13 ms
-- against 39 ms (make test-perf-search; docs/adr/0011-retrieval.md).

-- +goose Up
ALTER TABLE chunks ALTER COLUMN embedding SET STORAGE PLAIN;

-- +goose Down
ALTER TABLE chunks ALTER COLUMN embedding SET STORAGE EXTERNAL;
