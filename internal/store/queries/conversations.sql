-- Every query here is scoped by owner_id; TestOwnedQueriesAreScoped checks it.

-- name: CreateConversation :one
-- The owner's new conversation, about document when it is theirs, else over
-- their library. No row when document isn't the owner's.
INSERT INTO conversations (owner_id, document_id)
SELECT sqlc.arg(owner_id), sqlc.narg(document_id)::uuid
WHERE sqlc.narg(document_id)::uuid IS NULL
   OR EXISTS (SELECT 1 FROM documents d WHERE d.owner_id = sqlc.arg(owner_id) AND d.id = sqlc.narg(document_id)::uuid)
RETURNING id;

-- name: GetConversation :one
SELECT c.id, c.document_id, coalesce(d.title, '')::text AS document_title, c.title, c.created_at, c.updated_at
FROM conversations c
LEFT JOIN documents d ON d.id = c.document_id AND d.owner_id = c.owner_id
WHERE c.owner_id = sqlc.arg(owner_id) AND c.id = sqlc.arg(id);

-- name: ListConversations :many
-- The owner's conversations, most recently active first, after the
-- (updated_at, id) cursor.
SELECT c.id, c.document_id, coalesce(d.title, '')::text AS document_title, c.title, c.created_at, c.updated_at
FROM conversations c
LEFT JOIN documents d ON d.id = c.document_id AND d.owner_id = c.owner_id
WHERE c.owner_id = sqlc.arg(owner_id)
  AND (c.updated_at, c.id) < (sqlc.arg(before_updated_at)::timestamptz, sqlc.arg(before_id)::uuid)
ORDER BY c.updated_at DESC, c.id DESC
LIMIT sqlc.arg(max_rows);

-- name: DeleteConversation :execrows
DELETE FROM conversations
WHERE owner_id = $1 AND id = $2;

-- name: LockConversation :one
-- Holds the conversation until the transaction ends, so two answers can't
-- both start in it.
SELECT id FROM conversations
WHERE owner_id = $1 AND id = $2
FOR UPDATE;

-- name: TouchConversation :exec
-- Marks the conversation active now, and titles it with its first question.
UPDATE conversations
SET updated_at = now(), title = CASE WHEN title = '' THEN sqlc.arg(title) ELSE title END
WHERE owner_id = sqlc.arg(owner_id) AND id = sqlc.arg(id);

-- name: CountRunningAnswers :one
-- Answers in the conversation still streaming, unless they have been for
-- longer than any answer runs.
SELECT count(*)::int FROM messages
WHERE owner_id = sqlc.arg(owner_id) AND conversation_id = sqlc.arg(conversation_id) AND status = 'streaming'
  AND created_at > now() - make_interval(secs => sqlc.arg(running_for_seconds)::double precision);

-- name: InsertMessage :one
INSERT INTO messages (conversation_id, owner_id, role, status, content)
VALUES (sqlc.arg(conversation_id), sqlc.arg(owner_id), sqlc.arg(role), sqlc.arg(status), sqlc.arg(content))
RETURNING id, conversation_id, role, status, content, model, input_tokens, output_tokens,
    cache_creation_input_tokens, cache_read_input_tokens, cost_microusd, first_token_ms, latency_ms,
    tool_rounds, invalid_citations, self_markers, created_at;

-- name: ListMessages :many
-- A conversation's messages in order. An answer still streaming after
-- longer than any answer runs failed with its process.
SELECT id, conversation_id, role,
    CASE WHEN status = 'streaming' AND created_at <= now() - make_interval(secs => sqlc.arg(running_for_seconds)::double precision)
        THEN 'failed' ELSE status END::text AS status,
    content, model, input_tokens, output_tokens, cache_creation_input_tokens, cache_read_input_tokens,
    cost_microusd, first_token_ms, latency_ms, tool_rounds, invalid_citations, self_markers, created_at
FROM messages
WHERE owner_id = sqlc.arg(owner_id) AND conversation_id = sqlc.arg(conversation_id)
ORDER BY created_at, id;

-- name: FinishMessage :one
-- Ends a streaming answer with what it said and what it cost.
UPDATE messages
SET status = sqlc.arg(status), content = sqlc.arg(content), model = sqlc.arg(model),
    input_tokens = sqlc.arg(input_tokens), output_tokens = sqlc.arg(output_tokens),
    cache_creation_input_tokens = sqlc.arg(cache_creation_input_tokens),
    cache_read_input_tokens = sqlc.arg(cache_read_input_tokens), cost_microusd = sqlc.arg(cost_microusd),
    first_token_ms = sqlc.arg(first_token_ms), latency_ms = sqlc.arg(latency_ms),
    tool_rounds = sqlc.arg(tool_rounds), invalid_citations = sqlc.arg(invalid_citations),
    self_markers = sqlc.arg(self_markers), finished_at = now()
WHERE owner_id = sqlc.arg(owner_id) AND id = sqlc.arg(id) AND status = 'streaming'
RETURNING id, conversation_id, role, status, content, model, input_tokens, output_tokens,
    cache_creation_input_tokens, cache_read_input_tokens, cost_microusd, first_token_ms, latency_ms,
    tool_rounds, invalid_citations, self_markers, created_at;

-- name: ChatUsage :one
-- What counts against the owner's chat quotas within the window: questions
-- asked, and the input tokens their answers read, with how long until the
-- oldest of each leaves the window.
SELECT
    count(*) FILTER (WHERE role = 'user')::int AS questions,
    coalesce(sum(input_tokens + cache_creation_input_tokens + cache_read_input_tokens) FILTER (WHERE role = 'assistant'), 0)::bigint AS input_tokens,
    coalesce(extract(epoch FROM min(created_at) FILTER (WHERE role = 'user')
        + make_interval(secs => sqlc.arg(window_seconds)::double precision) - now()), 0)::double precision AS seconds_until_oldest_question_leaves,
    coalesce(extract(epoch FROM min(created_at) FILTER (WHERE role = 'assistant')
        + make_interval(secs => sqlc.arg(window_seconds)::double precision) - now()), 0)::double precision AS seconds_until_oldest_answer_leaves
FROM messages
WHERE owner_id = sqlc.arg(owner_id)
  AND created_at > now() - make_interval(secs => sqlc.arg(window_seconds)::double precision);
