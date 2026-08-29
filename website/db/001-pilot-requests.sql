-- Haechi pilot request storage for Azure Database for PostgreSQL.
--
-- Apply with:
--   psql "$HAECHI_DATABASE_URL" --single-transaction --file=db/001-pilot-requests.sql
--
-- This migration is idempotent so deployment can re-run it safely. It never
-- drops or rewrites existing data.
--
-- Data protection notes:
--   Consent is required to submit but is not stored, matching the original
--   behaviour. The honeypot field is never stored. Client IP addresses are
--   stored only as a keyed HMAC (ip_hash) used for rate limiting, never in raw
--   form. A retention and deletion schedule must be approved before public
--   launch; see dev-docs/engineering/readiness.md.

CREATE TABLE IF NOT EXISTS pilot_requests (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    reference    TEXT        NOT NULL UNIQUE,
    received_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    organisation TEXT        NOT NULL,
    work_email   TEXT        NOT NULL,
    role         TEXT        NOT NULL DEFAULT '',
    deployment   TEXT        NOT NULL,
    message      TEXT        NOT NULL DEFAULT '',
    user_agent   TEXT        NOT NULL DEFAULT '',
    ip_hash      TEXT        NOT NULL,

    CONSTRAINT pilot_requests_reference_format
        CHECK (reference ~ '^HCH-[0-9A-F]{8}$'),
    CONSTRAINT pilot_requests_organisation_length
        CHECK (char_length(organisation) BETWEEN 2 AND 120),
    CONSTRAINT pilot_requests_work_email_length
        CHECK (char_length(work_email) BETWEEN 3 AND 180),
    CONSTRAINT pilot_requests_deployment_allowed
        CHECK (deployment IN ('saas', 'dedicated', 'restricted', 'undecided')),
    CONSTRAINT pilot_requests_message_length
        CHECK (char_length(message) <= 1000),
    CONSTRAINT pilot_requests_user_agent_length
        CHECK (char_length(user_agent) <= 240),
    CONSTRAINT pilot_requests_ip_hash_format
        CHECK (ip_hash ~ '^[0-9a-f]{64}$')
);

CREATE INDEX IF NOT EXISTS pilot_requests_received_at_idx
    ON pilot_requests (received_at DESC);

-- Supports the rate-limit count, which is always scoped to one hash and window.
CREATE INDEX IF NOT EXISTS pilot_requests_ip_hash_received_at_idx
    ON pilot_requests (ip_hash, received_at DESC);
