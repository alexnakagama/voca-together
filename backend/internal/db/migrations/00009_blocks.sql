-- Blocks (decision 033): a member stops another member from reaching them.
-- While a row exists between two users, in either direction, neither is
-- served the other's profile or picture.

-- +goose Up
-- One row per direction: blocker_id blocked blocked_id. The reverse row is
-- another member's block and is theirs alone to remove. Both columns
-- reference users, not profiles: a block must outlive the blocked member's
-- profile being removed and saved again under a new public identifier, and it
-- goes with either account.
CREATE TABLE blocks (
    blocker_id uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    blocked_id uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(), -- orders a member's list, newest first
    PRIMARY KEY (blocker_id, blocked_id),
    CONSTRAINT blocks_not_self CHECK (blocker_id <> blocked_id)
);

-- The primary key holds both columns, so "is there a block between these
-- two" is an equality match on it for either order of the pair, and it
-- starts with the blocker, for a member's own list. Nothing in it starts
-- with blocked_id: this index is for the cascade when the blocked user is
-- deleted and for any query by blocked_id alone.
CREATE INDEX blocks_blocked_id_idx ON blocks (blocked_id);

-- +goose Down
-- Deletes every block. For development; a production rollback redeploys the
-- previous binary, which never touches this table and so enforces no block.
DROP TABLE blocks;
