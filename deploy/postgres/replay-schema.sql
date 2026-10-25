-- Haechi gateway replay schema (single source of truth for
-- adapters/postgres/replay.go). Records spent capability nonces so a token is
-- single-use across every verifier process in an audience
-- (Integration Document §6.3, §9 step 8; ADR-004).
--
-- TRUST ZONE. This schema MUST NOT be applied to the control-plane database
-- defined in schema.sql. The control-plane database holds tenants, devices and
-- identity-linked state; the gateway is designed never to reach it, and giving
-- a gateway credentials to it would defeat the separation the capability model
-- exists to provide (ADR-004: the gateway never receives durable identity).
--
-- Deploy it to a database, or at minimum a role, that can reach nothing else.
-- A least-privilege role needs exactly:
--
--   GRANT SELECT, INSERT, DELETE ON capability_replay TO haechi_gateway;
--
-- No column here identifies a subject. A nonce is random and unlinkable to
-- issuance by construction, and the epoch is a coarse hour-scale bucket shared
-- by every capability issued in it. Nothing recorded is a prohibited telemetry
-- field (threat-model/telemetry-prohibition-list.md).

BEGIN;

CREATE TABLE IF NOT EXISTS capability_replay (
    -- Epoch is uint64 in Go and BIGINT here. The adapter rejects any value
    -- above math.MaxInt64 rather than letting it wrap into a negative epoch,
    -- which would silently land in a different partition of the key space.
    epoch    BIGINT      NOT NULL,
    nonce    BYTEA       NOT NULL,
    spent_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- The primary key is the single-use guarantee. Two verifiers racing on the
    -- same token both attempt the insert; PostgreSQL admits exactly one, and
    -- the loser sees zero rows affected and denies. No advisory lock, no read
    -- before write, and no window between the check and the record.
    PRIMARY KEY (epoch, nonce)
);

-- Supports PurgeBefore, which deletes whole expired epochs. Without it the
-- purge degrades to a sequential scan of live admission data.
CREATE INDEX IF NOT EXISTS capability_replay_epoch_idx ON capability_replay (epoch);

COMMIT;
