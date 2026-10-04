-- +goose Up
-- Google ID tokens are no longer single-use (docs/decisions.md 026): Google
-- returns the same token to an app again while it is valid, so remembering
-- accepted tokens made every later Google sign-in fail until it expired. A
-- token is now accepted whenever it verifies, and nothing records its use.
DROP TABLE google_id_token_uses;

-- +goose Down
-- The table as 00003 created it, empty. Nothing reads or writes it after
-- this migration, so the uses of tokens still valid are not restored.
CREATE TABLE google_id_token_uses (
    token_hash bytea       PRIMARY KEY,
    expires_at timestamptz NOT NULL,
    CONSTRAINT google_id_token_uses_token_hash_length CHECK (octet_length(token_hash) = 32)
);
