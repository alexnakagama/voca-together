-- The public identifier of a profile (decision 031): the only thing that
-- names a member outside their own session. Random and opaque; it is not
-- users.id, which stays private, and it is not derived from it.

-- +goose Up
-- It lives on profiles, so a member who has saved no profile has no public
-- identity at all. The default is volatile: PostgreSQL rewrites the table and
-- gives every existing row its own value. updated_at is left alone, because
-- no profile's text changed.
ALTER TABLE profiles
    ADD COLUMN public_id uuid NOT NULL DEFAULT gen_random_uuid(),
    ADD CONSTRAINT profiles_public_id_key UNIQUE (public_id);

-- +goose Down
-- Drops every public identifier, and the constraint with the column; applying
-- the migration again gives each profile a new one. For development; a
-- production rollback redeploys the previous binary, which never reads the
-- column.
ALTER TABLE profiles DROP COLUMN public_id;
