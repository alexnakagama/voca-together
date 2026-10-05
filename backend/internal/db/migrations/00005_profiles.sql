-- User profiles (decision 027): what a member chooses to show of themselves.
-- display_name and bio are meant to become visible to other members; nothing
-- else about a user is.

-- +goose Up
-- One row per user who has saved a profile; no row means "no profile yet".
-- The primary key is the owner, so a user can never have two, and it is the
-- only lookup key (no other index).
CREATE TABLE profiles (
    user_id      uuid        PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    -- Stored normalized by the application (NFC, trimmed, single spaces). The
    -- CHECKs keep any other writer from storing unbounded or padded input;
    -- char_length counts characters, as the application does.
    display_name text        NOT NULL,
    bio          text        NOT NULL DEFAULT '', -- '' = none; never NULL
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(), -- last change of display_name or bio
    CONSTRAINT profiles_display_name_length  CHECK (char_length(display_name) BETWEEN 1 AND 50),
    CONSTRAINT profiles_display_name_trimmed CHECK (display_name = btrim(display_name)),
    CONSTRAINT profiles_bio_length           CHECK (char_length(bio) <= 500)
);

-- +goose Down
-- Deletes every profile. For development; a production rollback redeploys the
-- previous binary, which never touches this table.
DROP TABLE profiles;
