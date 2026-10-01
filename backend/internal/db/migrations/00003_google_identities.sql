-- Google sign-in (decision 020): passwordless users, external identities and
-- single-use Google ID tokens.

-- +goose Up
-- Accounts created with Google have no password. The CHECK keeps the empty
-- string from standing in for "no password".
ALTER TABLE users
    ALTER COLUMN password_hash DROP NOT NULL,
    ADD CONSTRAINT users_password_hash_not_empty CHECK (password_hash IS NULL OR password_hash <> '');

-- External identities that sign a user in, keyed by the provider's stable
-- subject (Google `sub`), never by email.
CREATE TABLE user_identities (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    provider   text        NOT NULL,
    subject    text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT user_identities_provider_check       CHECK (provider IN ('google')),
    CONSTRAINT user_identities_subject_length       CHECK (octet_length(subject) BETWEEN 1 AND 255),
    -- One owner per identity; also the sign-in lookup index.
    CONSTRAINT user_identities_provider_subject_key UNIQUE (provider, subject),
    -- One identity per provider and user; also indexes the foreign key.
    CONSTRAINT user_identities_user_provider_key    UNIQUE (user_id, provider)
);

-- Every Google ID token accepted, so each can sign in only once (replay
-- protection). Only the SHA-256 is stored; rows are deleted once expired.
CREATE TABLE google_id_token_uses (
    token_hash bytea       PRIMARY KEY,
    expires_at timestamptz NOT NULL,
    CONSTRAINT google_id_token_uses_token_hash_length CHECK (octet_length(token_hash) = 32)
);

-- Every user keeps at least one way to sign in: a password or an identity.
-- Checked at commit (deferred), so a passwordless user and its identity can
-- be inserted in one transaction. A deleted user passes (nothing is left to
-- check). Under READ COMMITTED the check can't see other transactions'
-- uncommitted changes, so any flow removing a method must also lock the user
-- row FOR UPDATE.
-- +goose StatementBegin
CREATE FUNCTION users_require_auth_method() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    uid uuid;
BEGIN
    IF TG_TABLE_NAME = 'users' THEN
        uid := NEW.id;
    ELSE
        uid := OLD.user_id;
    END IF;
    IF EXISTS (
        SELECT 1 FROM users u
        WHERE u.id = uid AND u.password_hash IS NULL
          AND NOT EXISTS (SELECT 1 FROM user_identities i WHERE i.user_id = u.id)
    ) THEN
        RAISE EXCEPTION 'user has no authentication method'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'users_auth_method_required';
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE CONSTRAINT TRIGGER users_auth_method_required
    AFTER INSERT OR UPDATE OF password_hash ON users
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION users_require_auth_method();

CREATE CONSTRAINT TRIGGER user_identities_auth_method_required
    AFTER DELETE OR UPDATE OF user_id ON user_identities
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION users_require_auth_method();

-- +goose Down
DROP TABLE google_id_token_uses;
DROP TABLE user_identities;
DROP TRIGGER users_auth_method_required ON users;
DROP FUNCTION users_require_auth_method();
-- Fails if passwordless users exist, rather than deleting them.
ALTER TABLE users
    DROP CONSTRAINT users_password_hash_not_empty,
    ALTER COLUMN password_hash SET NOT NULL;
