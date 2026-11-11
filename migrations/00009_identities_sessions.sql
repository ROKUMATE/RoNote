-- Sign-in and sessions (docs/adr/0010-sign-in-and-sessions.md). A user is
-- an identity at a provider, never an email; a session is a random token of
-- which only the SHA-256 is kept. Both go with their user.

-- +goose Up
CREATE TABLE user_identities (
    provider text NOT NULL,
    provider_user_id text NOT NULL,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- The provider's name for the user, refreshed at every sign-in.
    login text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, provider_user_id)
);

CREATE INDEX user_identities_user_idx ON user_identities (user_id);

CREATE TABLE sessions (
    token_hash bytea PRIMARY KEY CHECK (octet_length(token_hash) = 32),
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    -- The absolute limit; the idle limit counts from last_seen_at.
    expires_at timestamptz NOT NULL
);

CREATE INDEX sessions_user_idx ON sessions (user_id);

-- +goose Down
DROP TABLE sessions;
DROP TABLE user_identities;
