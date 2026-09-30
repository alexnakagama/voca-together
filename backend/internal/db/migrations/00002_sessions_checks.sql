-- Milestone 1 step 6: invariants for sessions created by login.

-- +goose Up
ALTER TABLE sessions
    -- The application truncates the User-Agent header to 256 bytes; the
    -- CHECK keeps any other writer from storing unbounded client input.
    ADD CONSTRAINT sessions_user_agent_length
        CHECK (user_agent IS NULL OR octet_length(user_agent) <= 256),
    -- No token may outlive the session's absolute lifetime. Refresh (which
    -- slides refresh_expires_at) must cap both expiries at expires_at.
    ADD CONSTRAINT sessions_expiry_order
        CHECK (access_expires_at <= expires_at AND refresh_expires_at <= expires_at);

-- +goose Down
ALTER TABLE sessions
    DROP CONSTRAINT sessions_expiry_order,
    DROP CONSTRAINT sessions_user_agent_length;
