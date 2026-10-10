-- Reports (decision 033): a member tells the people who run the service about
-- another member. A report is written through the API and read only with
-- database access (docs/moderation.md); no route returns one.

-- +goose Up
-- One row per reporter and reported member: a later report by the same member
-- replaces the content of the earlier one. A report holds who, about whom,
-- why and when, and no copy of the reported member's name, text, languages or
-- picture.
--
-- The two foreign keys differ on purpose. A report goes with the account it
-- is about, and stays when its author's account goes, with no reporter. The
-- pair therefore can't be the primary key: id is a surrogate that is never
-- returned. A NULL reporter is distinct from every other in the UNIQUE
-- constraint, so several such rows about one member can coexist, and
-- reports_not_self passes on NULL.
CREATE TABLE reports (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    reporter_id uuid        REFERENCES users (id) ON DELETE SET NULL,
    reported_id uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    reason      text        NOT NULL,
    details     text        NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(), -- moves when the content is replaced
    CONSTRAINT reports_reporter_reported_key UNIQUE (reporter_id, reported_id),
    CONSTRAINT reports_not_self CHECK (reporter_id <> reported_id),
    -- Text with a CHECK and not an enum type: adding a reason is a migration
    -- that replaces this constraint.
    CONSTRAINT reports_reason CHECK (reason IN ('harassment', 'inappropriate_content', 'spam', 'impersonation', 'other')),
    CONSTRAINT reports_details_length CHECK (char_length(details) <= 1000)
);

-- How reports are read (per reported member), and the cascade when that
-- member's account is deleted. The UNIQUE constraint starts with the reporter.
CREATE INDEX reports_reported_id_idx ON reports (reported_id);

-- +goose Down
-- Deletes every report. For development; a production rollback redeploys the
-- previous binary, which never touches this table.
DROP TABLE reports;
