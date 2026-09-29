-- Milestone 1: identity & authentication.
-- All secret tokens are stored only as SHA-256 hashes (32 bytes), never raw.

-- +goose Up
CREATE TABLE users (
    id                uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Stored normalized (trimmed, lowercased) by the application; the CHECK
    -- guarantees it, so the plain UNIQUE constraint is case-insensitive.
    email             text        NOT NULL,
    password_hash     text        NOT NULL, -- argon2id, PHC string format
    email_verified_at timestamptz,          -- NULL = unverified
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT users_email_key    UNIQUE (email),
    CONSTRAINT users_email_lower  CHECK (email = lower(email)),
    CONSTRAINT users_email_length CHECK (char_length(email) BETWEEN 3 AND 254)
);

-- Single-use tokens for email verification and password reset.
CREATE TABLE user_tokens (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    purpose    text        NOT NULL,
    token_hash bytea       NOT NULL,
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,            -- set atomically when consumed
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT user_tokens_purpose_check      CHECK (purpose IN ('email_verification', 'password_reset')),
    CONSTRAINT user_tokens_token_hash_length  CHECK (octet_length(token_hash) = 32),
    CONSTRAINT user_tokens_token_hash_key     UNIQUE (token_hash) -- lookup index
);

-- At most one unused token per user and purpose: issuing a new one must
-- delete the previous one, so old links stop working.
CREATE UNIQUE INDEX user_tokens_one_active ON user_tokens (user_id, purpose) WHERE used_at IS NULL;

-- One row per login (device). Refresh rotates the token hashes in place.
CREATE TABLE sessions (
    id                          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                     uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    access_token_hash           bytea       NOT NULL,
    access_expires_at           timestamptz NOT NULL,
    refresh_token_hash          bytea       NOT NULL,
    refresh_expires_at          timestamptz NOT NULL,
    -- The last rotated-out refresh token; presenting it again signals theft.
    previous_refresh_token_hash bytea,
    expires_at                  timestamptz NOT NULL, -- absolute session lifetime cap
    revoked_at                  timestamptz,
    user_agent                  text,
    created_at                  timestamptz NOT NULL DEFAULT now(),
    last_used_at                timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT sessions_access_token_hash_length  CHECK (octet_length(access_token_hash) = 32),
    CONSTRAINT sessions_refresh_token_hash_length CHECK (octet_length(refresh_token_hash) = 32),
    CONSTRAINT sessions_previous_refresh_token_hash_length
        CHECK (previous_refresh_token_hash IS NULL OR octet_length(previous_refresh_token_hash) = 32),
    CONSTRAINT sessions_access_token_hash_key           UNIQUE (access_token_hash),
    CONSTRAINT sessions_refresh_token_hash_key          UNIQUE (refresh_token_hash),
    CONSTRAINT sessions_previous_refresh_token_hash_key UNIQUE (previous_refresh_token_hash)
);

-- Supports revoking all of a user's live sessions (password reset).
CREATE INDEX sessions_user_active ON sessions (user_id) WHERE revoked_at IS NULL;

-- +goose Down
DROP TABLE sessions;
DROP TABLE user_tokens;
DROP TABLE users;
