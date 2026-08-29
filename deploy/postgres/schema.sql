-- Haechi control-plane schema (single source of truth for adapters/postgres).
-- Durable state for tenancy, device inventory, gateways and policy metadata
-- (Integration Document §5 postgres row). Arrays are stored as jsonb so the Go
-- adapter stays driver-agnostic.
--
-- Multi-tenant isolation: application always scopes by tenant_id; enable Row
-- Level Security for defence in depth in shared deployments (§10.1).

BEGIN;

CREATE TABLE IF NOT EXISTS tenants (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    idp_issuer  TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS enrollment_codes (
    code       TEXT PRIMARY KEY, -- SHA-256 lookup key; raw credential is never stored
    tenant_id  TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    challenge  BYTEA NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    used       BOOLEAN NOT NULL DEFAULT false
);

CREATE TABLE IF NOT EXISTS devices (
    id          TEXT PRIMARY KEY,
    tenant_id   TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    public_key  BYTEA NOT NULL,          -- raw Ed25519 (32 bytes)
    platform    TEXT NOT NULL DEFAULT '',
    posture     TEXT NOT NULL DEFAULT '',
    enrolled_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked     BOOLEAN NOT NULL DEFAULT false,
    revoked_at  TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS devices_tenant_idx ON devices(tenant_id);

CREATE TABLE IF NOT EXISTS gateways (
    id                   TEXT PRIMARY KEY,
    tenant_id            TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    region               TEXT NOT NULL DEFAULT '',
    audience             TEXT NOT NULL DEFAULT '',
    service_classes      JSONB NOT NULL DEFAULT '[]'::jsonb,
    endpoint             TEXT NOT NULL DEFAULT '',
    wireguard_public_key TEXT NOT NULL DEFAULT '',
    pq_capable           BOOLEAN NOT NULL DEFAULT false,
    healthy              BOOLEAN NOT NULL DEFAULT false,
    load                 DOUBLE PRECISION NOT NULL DEFAULT 0,
    last_seen            TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS gateways_tenant_audience_idx ON gateways(tenant_id, audience);

CREATE TABLE IF NOT EXISTS tenant_policies (
    tenant_id               TEXT PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    allowed_service_classes JSONB NOT NULL DEFAULT '[]'::jsonb,
    allowed_transports      JSONB NOT NULL DEFAULT '[]'::jsonb,
    pq_requirement          TEXT NOT NULL DEFAULT 'preferred',
    kill_switch             BOOLEAN NOT NULL DEFAULT true,
    dns                     JSONB NOT NULL DEFAULT '[]'::jsonb,
    version                 BIGINT NOT NULL DEFAULT 1
);

COMMIT;
