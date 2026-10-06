-- Profile pictures (decision 031). A member's picture is personal data that,
-- like the profile's text, other signed-in members may see once the member
-- has a profile.

-- +goose Up
-- One row per user who has a picture; no row means "no picture". The primary
-- key is the owner, so a user can never have two, and it is the only lookup
-- key. It references users, not profiles: a picture can be set before a
-- profile is saved, and whether others see it is decided where it is served.
CREATE TABLE avatars (
    user_id       uuid        PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    -- Never the bytes a member uploaded: the picture the server produced from
    -- the decoded pixels, a 512x512 JPEG with no metadata. The CHECK keeps any
    -- other writer from storing something unbounded; 512 KiB is several times
    -- what such a JPEG takes.
    image         bytea       NOT NULL,
    -- SHA-256 of the bytes that were uploaded, kept so that the same upload
    -- sent again is recognised and changes nothing. Never returned or logged.
    source_sha256 bytea       NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(), -- last change of the picture
    CONSTRAINT avatars_image_size           CHECK (octet_length(image) BETWEEN 1 AND 524288),
    CONSTRAINT avatars_source_sha256_length CHECK (octet_length(source_sha256) = 32)
);

-- +goose Down
-- Deletes every picture. For development; a production rollback redeploys the
-- previous binary, which never touches this table.
DROP TABLE avatars;
