-- Chat over the library (docs/adr/0012-chat.md): a user's conversations,
-- each over their whole library or one document, and their messages. Both
-- go with their user, and a document's conversations go with it.

-- +goose Up
CREATE TABLE conversations (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    owner_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- The document the conversation is about, or null for the library.
    document_id uuid,
    -- The first question, cut; '' until there is one.
    title text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT conversations_id_owner_key UNIQUE (id, owner_id),
    CONSTRAINT conversations_document_fkey FOREIGN KEY (document_id, owner_id)
        REFERENCES documents (id, owner_id) ON DELETE CASCADE
);

-- The owner's conversations, most recently active first.
CREATE INDEX conversations_owner_idx ON conversations (owner_id, updated_at DESC, id DESC);
CREATE INDEX conversations_document_idx ON conversations (document_id, owner_id) WHERE document_id IS NOT NULL;

CREATE TABLE messages (
    id uuid PRIMARY KEY DEFAULT uuidv7(),
    conversation_id uuid NOT NULL,
    owner_id uuid NOT NULL,
    role text NOT NULL CHECK (role IN ('user', 'assistant')),
    -- An answer is streaming until it is done, stopped or failed; a
    -- question is done when it is written.
    status text NOT NULL CHECK (status IN ('streaming', 'done', 'stopped', 'failed')),
    -- Text segments, and the citations an answer's segments point at.
    content jsonb NOT NULL,
    -- What an answer cost: its model, tokens across all its calls, in
    -- micro-dollars, and how long it took. Zero for a question.
    model text NOT NULL DEFAULT '',
    input_tokens integer NOT NULL DEFAULT 0,
    output_tokens integer NOT NULL DEFAULT 0,
    cache_creation_input_tokens integer NOT NULL DEFAULT 0,
    cache_read_input_tokens integer NOT NULL DEFAULT 0,
    cost_microusd bigint NOT NULL DEFAULT 0,
    first_token_ms integer NOT NULL DEFAULT 0,
    latency_ms integer NOT NULL DEFAULT 0,
    tool_rounds integer NOT NULL DEFAULT 0,
    -- Citations the model made that pointed at nothing it was sent.
    invalid_citations integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    CONSTRAINT messages_conversation_fkey FOREIGN KEY (conversation_id, owner_id)
        REFERENCES conversations (id, owner_id) ON DELETE CASCADE
);

CREATE INDEX messages_conversation_idx ON messages (conversation_id, created_at, id);
-- The chat quotas count one owner's recent messages.
CREATE INDEX messages_owner_idx ON messages (owner_id, created_at);

-- +goose Down
DROP TABLE messages;
DROP TABLE conversations;
