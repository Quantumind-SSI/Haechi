// Storage adapter tests.
//
// The postgres tests run only when HAECHI_TEST_DATABASE_URL points at a
// disposable PostgreSQL database. Without it they are skipped, so the suite
// stays runnable offline. CI supplies the variable from a service container.
//
// Start a throwaway server locally with:
//   initdb -D /tmp/haechi-pg -U postgres --auth=trust
//   pg_ctl -D /tmp/haechi-pg -o "-p 55432" -l /tmp/haechi-pg/server.log start
//   createdb -h 127.0.0.1 -p 55432 -U postgres haechi_pilot
//   export HAECHI_TEST_DATABASE_URL="postgresql://postgres@127.0.0.1:55432/haechi_pilot"

import assert from "node:assert/strict";
import test from "node:test";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  RATE_LIMIT,
  RateLimitedError,
  StorageUnavailableError,
  createFileStorage,
  createPostgresStorage,
  generateReference,
} from "../storage.mjs";

const databaseUrl = process.env.HAECHI_TEST_DATABASE_URL;

const VALID_REQUEST = {
  organisation: "Example CNI Ltd",
  workEmail: "security@example.test",
  role: "CISO",
  deployment: "dedicated",
  message: "Controlled pilot request.",
};

const IP_HASH = "a".repeat(64);

function submission(ipHash = IP_HASH) {
  return { request: VALID_REQUEST, userAgent: "test-agent", ipHash };
}

test("references match the published format", () => {
  assert.match(generateReference(), /^HCH-[0-9A-F]{8}$/);
});

test("file storage persists a submission and omits consent", async () => {
  const dataDir = await mkdtemp(join(tmpdir(), "haechi-storage-"));
  const storage = createFileStorage({ dataDir });
  try {
    const { reference } = await storage.save(submission());
    assert.match(reference, /^HCH-[0-9A-F]{8}$/);

    const stored = await readFile(
      join(dataDir, "pilot-requests.ndjson"),
      "utf8",
    );
    assert.match(stored, /Example CNI Ltd/);
    assert.doesNotMatch(stored, /"consent"/);
    assert.doesNotMatch(stored, /"website"/);
  } finally {
    await storage.close();
    await rm(dataDir, { recursive: true, force: true });
  }
});

test("file storage enforces the rate limit", async () => {
  const dataDir = await mkdtemp(join(tmpdir(), "haechi-storage-"));
  const storage = createFileStorage({ dataDir });
  try {
    for (let attempt = 0; attempt < RATE_LIMIT; attempt += 1) {
      await storage.save(submission());
    }
    await assert.rejects(storage.save(submission()), RateLimitedError);

    // A different caller is unaffected.
    const other = await storage.save(submission("b".repeat(64)));
    assert.match(other.reference, /^HCH-/);
  } finally {
    await storage.close();
    await rm(dataDir, { recursive: true, force: true });
  }
});

test("file storage fails closed when the directory cannot be written", async () => {
  // Use a regular file as the parent path. Creating a directory beneath it
  // fails with ENOTDIR on every platform and regardless of privilege, unlike
  // permission tricks or special filesystems such as /proc, whose behaviour
  // varies between macOS and Linux.
  const parent = await mkdtemp(join(tmpdir(), "haechi-storage-"));
  const blocker = join(parent, "not-a-directory");
  await writeFile(blocker, "");

  const storage = createFileStorage({ dataDir: join(blocker, "data") });
  try {
    await assert.rejects(storage.save(submission()), StorageUnavailableError);
    await assert.rejects(storage.ping(), StorageUnavailableError);
  } finally {
    await storage.close();
    await rm(parent, { recursive: true, force: true });
  }
});

test("postgres storage rejects a pool without query()", () => {
  assert.throws(() => createPostgresStorage({ pool: {} }), TypeError);
});

test("postgres storage surfaces driver failures as unavailable", async () => {
  const storage = createPostgresStorage({
    pool: {
      query() {
        throw new Error("connection reset");
      },
    },
  });
  await assert.rejects(storage.save(submission()), StorageUnavailableError);
});

test("postgres storage treats a suppressed insert as rate limited", async () => {
  const storage = createPostgresStorage({
    pool: {
      async query() {
        return { rowCount: 0, rows: [] };
      },
    },
  });
  await assert.rejects(storage.save(submission()), RateLimitedError);
});

test(
  "postgres storage persists, rate limits, and resists concurrent races",
  { skip: databaseUrl ? false : "HAECHI_TEST_DATABASE_URL is not set" },
  async (t) => {
    const { default: pg } = await import("pg");
    const pool = new pg.Pool({ connectionString: databaseUrl, max: 8 });
    const storage = createPostgresStorage({ pool });
    const ipHash = "c".repeat(64);

    t.after(async () => {
      await pool.query("DELETE FROM pilot_requests WHERE ip_hash = $1", [
        ipHash,
      ]);
      await storage.close();
    });

    await pool.query("DELETE FROM pilot_requests WHERE ip_hash = $1", [ipHash]);

    const first = await storage.save(submission(ipHash));
    assert.match(first.reference, /^HCH-[0-9A-F]{8}$/);

    const stored = await pool.query(
      "SELECT organisation, work_email, deployment FROM pilot_requests WHERE reference = $1",
      [first.reference],
    );
    assert.equal(stored.rowCount, 1);
    assert.equal(stored.rows[0].organisation, VALID_REQUEST.organisation);
    assert.equal(stored.rows[0].work_email, VALID_REQUEST.workEmail);

    // Saturate the remaining budget, then fire the next batch concurrently.
    for (let attempt = 1; attempt < RATE_LIMIT; attempt += 1) {
      await storage.save(submission(ipHash));
    }

    const contended = await Promise.allSettled(
      Array.from({ length: 8 }, () => storage.save(submission(ipHash))),
    );
    const accepted = contended.filter(
      (outcome) => outcome.status === "fulfilled",
    );
    const rejected = contended.filter(
      (outcome) =>
        outcome.status === "rejected" &&
        outcome.reason instanceof RateLimitedError,
    );

    assert.equal(accepted.length, 0, "budget was already exhausted");
    assert.equal(rejected.length, 8);

    const total = await pool.query(
      "SELECT count(*)::int AS n FROM pilot_requests WHERE ip_hash = $1",
      [ipHash],
    );
    assert.equal(
      total.rows[0].n,
      RATE_LIMIT,
      "concurrent submissions must never exceed the rate limit",
    );
  },
);
