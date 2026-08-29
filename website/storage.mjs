// Pilot-request storage adapters.
//
// The site runs in two shapes:
//   local development  -> file adapter, append-only NDJSON under runtime/
//   Azure Container Apps -> postgres adapter, Azure Database for PostgreSQL
//
// Both adapters expose the same contract so server.mjs never branches on
// deployment target:
//
//   save({ request, userAgent, ipHash })  -> { reference }
//   ping()                                -> resolves when storage is usable
//   close()                               -> releases resources
//
// The postgres adapter enforces the per-hash rate limit inside a single
// statement, so two concurrent submissions cannot both pass the same limit.
// It fails closed: any storage error rejects the submission rather than
// silently dropping an enterprise lead.

import { appendFile, chmod, mkdir } from "node:fs/promises";
import { join } from "node:path";
import { randomUUID } from "node:crypto";

export const RATE_LIMIT = 5;
export const RATE_WINDOW_MS = 60 * 60 * 1000;

/** Raised when the caller exceeded the per-hash submission budget. */
export class RateLimitedError extends Error {
  constructor() {
    super("Too many requests. Please try again later.");
    this.name = "RateLimitedError";
  }
}

/** Raised when storage is unavailable or refused the write. */
export class StorageUnavailableError extends Error {
  constructor(cause) {
    super("Submissions are temporarily unavailable.");
    this.name = "StorageUnavailableError";
    this.cause = cause;
  }
}

export function generateReference() {
  return `HCH-${randomUUID().slice(0, 8).toUpperCase()}`;
}

// ---------------------------------------------------------------------------
// File adapter (local development only)
// ---------------------------------------------------------------------------

/**
 * Append-only NDJSON storage. Used for local development. It keeps an
 * in-process rate limiter because the file itself is not queryable.
 */
export function createFileStorage({ dataDir, maximumClients = 10_000 }) {
  if (typeof dataDir !== "string" || dataDir === "") {
    throw new TypeError("createFileStorage requires a dataDir");
  }
  const clients = new Map();

  function allow(ipHash, now) {
    const existing = clients.get(ipHash);
    if (!existing || existing.resetAt <= now) {
      if (!existing && clients.size >= maximumClients) {
        for (const [key, limit] of clients) {
          if (limit.resetAt <= now) clients.delete(key);
        }
        if (clients.size >= maximumClients) return false;
      }
      clients.set(ipHash, { count: 1, resetAt: now + RATE_WINDOW_MS });
      return true;
    }
    if (existing.count >= RATE_LIMIT) return false;
    existing.count += 1;
    return true;
  }

  return {
    kind: "file",

    async save({ request, userAgent, ipHash }) {
      if (!allow(ipHash, Date.now())) throw new RateLimitedError();

      const reference = generateReference();
      const record = {
        reference,
        receivedAt: new Date().toISOString(),
        organisation: request.organisation,
        workEmail: request.workEmail,
        role: request.role,
        deployment: request.deployment,
        message: request.message,
        userAgent,
        ipHash,
      };

      try {
        await mkdir(dataDir, { recursive: true, mode: 0o700 });
        const outputPath = join(dataDir, "pilot-requests.ndjson");
        await appendFile(outputPath, `${JSON.stringify(record)}\n`, {
          encoding: "utf8",
          mode: 0o600,
        });
        await chmod(outputPath, 0o600);
      } catch (error) {
        throw new StorageUnavailableError(error);
      }
      return { reference };
    },

    async ping() {
      try {
        await mkdir(dataDir, { recursive: true, mode: 0o700 });
      } catch (error) {
        throw new StorageUnavailableError(error);
      }
    },

    async close() {
      clients.clear();
    },
  };
}

// ---------------------------------------------------------------------------
// Postgres adapter (Azure Database for PostgreSQL)
// ---------------------------------------------------------------------------

// Counts prior submissions for this hash inside the window and inserts only if
// the caller is under budget. One statement, so the check cannot be raced.
const INSERT_IF_UNDER_LIMIT = `
INSERT INTO pilot_requests
  (reference, received_at, organisation, work_email, role, deployment, message, user_agent, ip_hash)
SELECT $1, now(), $2, $3, $4, $5, $6, $7, $8
WHERE (
  SELECT count(*) FROM pilot_requests
  WHERE ip_hash = $8 AND received_at > now() - ($9::bigint * INTERVAL '1 millisecond')
) < $10
RETURNING reference
`;

/**
 * Durable storage backed by PostgreSQL.
 *
 * `pool` must expose `query(text, values)` and `end()`, which both `pg.Pool`
 * and a test double satisfy. Injecting it keeps this module free of a hard
 * dependency on the driver and makes the SQL contract testable.
 */
export function createPostgresStorage({ pool }) {
  if (!pool || typeof pool.query !== "function") {
    throw new TypeError("createPostgresStorage requires a pool with query()");
  }

  return {
    kind: "postgres",

    async save({ request, userAgent, ipHash }) {
      const reference = generateReference();
      let result;
      try {
        result = await pool.query(INSERT_IF_UNDER_LIMIT, [
          reference,
          request.organisation,
          request.workEmail,
          request.role,
          request.deployment,
          request.message,
          userAgent,
          ipHash,
          RATE_WINDOW_MS,
          RATE_LIMIT,
        ]);
      } catch (error) {
        throw new StorageUnavailableError(error);
      }
      if (!result || result.rowCount !== 1) throw new RateLimitedError();
      return { reference };
    },

    async ping() {
      try {
        await pool.query("SELECT 1");
      } catch (error) {
        throw new StorageUnavailableError(error);
      }
    },

    async close() {
      if (typeof pool.end === "function") await pool.end();
    },
  };
}

/**
 * Opens a PostgreSQL pool against Azure Database for PostgreSQL.
 *
 * TLS is mandatory. Azure terminates TLS with a public certificate authority,
 * so certificate verification stays on; there is no insecure fallback.
 *
 * The `pg` driver is imported dynamically so local development and the test
 * suite run without it installed.
 */
export async function openPostgresPool({
  connectionString,
  maxConnections = 4,
}) {
  if (typeof connectionString !== "string" || connectionString === "") {
    throw new TypeError("openPostgresPool requires a connection string");
  }
  const { default: pg } = await import("pg");
  return new pg.Pool({
    connectionString,
    max: maxConnections,
    ssl: { rejectUnauthorized: true },
    connectionTimeoutMillis: 10_000,
    idleTimeoutMillis: 30_000,
    application_name: "haechi-website",
  });
}
