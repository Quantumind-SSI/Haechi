// Origin server tests covering the Cloudflare-to-Azure split: cross-origin
// policy, proxy trust, readiness, and every submission failure path.

import assert from "node:assert/strict";
import test from "node:test";
import { RateLimitedError, StorageUnavailableError } from "../storage.mjs";
import {
  clientAddress,
  createHaechiServer,
  hashIp,
  validatePilotRequest,
} from "../server.mjs";

const SALT = "0123456789abcdef0123456789abcdef";
const ALLOWED_ORIGIN = "https://haechi.example";

const VALID_BODY = {
  organisation: "Example CNI Ltd",
  workEmail: "security@example.test",
  role: "CISO",
  deployment: "dedicated",
  message: "Controlled pilot request.",
  consent: true,
  website: "",
};

function recordingStorage(overrides = {}) {
  const saved = [];
  return {
    kind: "test",
    saved,
    async save(submission) {
      saved.push(submission);
      if (overrides.saveError) throw overrides.saveError;
      return { reference: "HCH-ABCD1234" };
    },
    async ping() {
      if (overrides.pingError) throw overrides.pingError;
    },
    async close() {},
  };
}

async function withServer(options, run) {
  const server = createHaechiServer({ ipSalt: SALT, ...options });
  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  const baseUrl = `http://127.0.0.1:${server.address().port}`;
  try {
    await run(baseUrl);
  } finally {
    await new Promise((resolve) => server.close(resolve));
  }
}

function postPilot(baseUrl, { origin, body = VALID_BODY, headers = {} } = {}) {
  return fetch(`${baseUrl}/api/pilot`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      ...(origin ? { Origin: origin } : {}),
      ...headers,
    },
    body: JSON.stringify(body),
  });
}

test("client pseudonyms are keyed and stable", () => {
  const first = hashIp("203.0.113.1", SALT);
  assert.equal(first, hashIp("203.0.113.1", SALT));
  assert.notEqual(first, hashIp("203.0.113.2", SALT));
  assert.notEqual(first, hashIp("203.0.113.1", "a".repeat(32)));
  assert.match(first, /^[0-9a-f]{64}$/);
});

test("forwarded addresses are ignored unless the ingress is trusted", () => {
  const request = {
    headers: { "x-forwarded-for": "198.51.100.9, 203.0.113.7" },
    socket: { remoteAddress: "10.0.0.1" },
  };
  assert.equal(clientAddress(request, false), "10.0.0.1");
  assert.equal(clientAddress(request, true), "198.51.100.9");
});

test("the honeypot field rejects automated submissions", () => {
  const { errors } = validatePilotRequest({ ...VALID_BODY, website: "spam" });
  assert.equal(errors.website, "Automated submission rejected.");
});

test("a short pseudonym key is refused at construction", () => {
  assert.throws(
    () => createHaechiServer({ ipSalt: "too-short" }),
    /at least 32/,
  );
});

test("readiness reflects storage health", async () => {
  await withServer({ storage: recordingStorage() }, async (baseUrl) => {
    const healthy = await fetch(`${baseUrl}/api/health`);
    assert.equal(healthy.status, 200);
    assert.deepEqual(await healthy.json(), {
      status: "ok",
      service: "haechi-website",
    });
  });

  const failing = recordingStorage({
    pingError: new StorageUnavailableError(new Error("no route to host")),
  });
  await withServer({ storage: failing }, async (baseUrl) => {
    const response = await fetch(`${baseUrl}/api/health`);
    assert.equal(response.status, 503);
    assert.equal((await response.json()).status, "unavailable");
  });
});

test("an accepted submission is pseudonymised before storage", async () => {
  const storage = recordingStorage();
  await withServer({ storage }, async (baseUrl) => {
    const response = await postPilot(baseUrl);
    assert.equal(response.status, 201);
    assert.equal((await response.json()).reference, "HCH-ABCD1234");
  });

  assert.equal(storage.saved.length, 1);
  const submission = storage.saved[0];
  assert.match(submission.ipHash, /^[0-9a-f]{64}$/);
  assert.equal(submission.request.organisation, VALID_BODY.organisation);
  assert.equal(submission.request.consent, true);
  assert.equal(submission.userAgent.length <= 240, true);
});

test("cross-origin submissions are restricted to the configured origins", async () => {
  const options = {
    storage: recordingStorage(),
    allowedOrigins: [ALLOWED_ORIGIN],
  };

  await withServer(options, async (baseUrl) => {
    const preflight = await fetch(`${baseUrl}/api/pilot`, {
      method: "OPTIONS",
      headers: { Origin: ALLOWED_ORIGIN },
    });
    assert.equal(preflight.status, 204);
    assert.equal(
      preflight.headers.get("access-control-allow-origin"),
      ALLOWED_ORIGIN,
    );
    assert.equal(preflight.headers.get("vary"), "Origin");

    const deniedPreflight = await fetch(`${baseUrl}/api/pilot`, {
      method: "OPTIONS",
      headers: { Origin: "https://attacker.example" },
    });
    assert.equal(deniedPreflight.status, 403);

    const allowed = await postPilot(baseUrl, { origin: ALLOWED_ORIGIN });
    assert.equal(allowed.status, 201);

    const denied = await postPilot(baseUrl, {
      origin: "https://attacker.example",
    });
    assert.equal(denied.status, 403);
  });
});

test("the content security policy advertises permitted API origins", async () => {
  await withServer(
    { storage: recordingStorage(), allowedOrigins: [ALLOWED_ORIGIN] },
    async (baseUrl) => {
      const response = await fetch(`${baseUrl}/api/health`);
      const policy = response.headers.get("content-security-policy");
      assert.match(policy, new RegExp(`connect-src 'self' ${ALLOWED_ORIGIN}`));
      assert.match(policy, /frame-ancestors 'none'/);
    },
  );
});

test("submission failures map to precise status codes", async () => {
  await withServer({ storage: recordingStorage() }, async (baseUrl) => {
    const wrongType = await fetch(`${baseUrl}/api/pilot`, {
      method: "POST",
      headers: { "Content-Type": "text/plain" },
      body: "{}",
    });
    assert.equal(wrongType.status, 415);

    const invalid = await postPilot(baseUrl, { body: {} });
    assert.equal(invalid.status, 422);

    const notAllowed = await fetch(`${baseUrl}/api/pilot`, {
      method: "DELETE",
    });
    assert.equal(notAllowed.status, 405);
  });

  const limited = recordingStorage({ saveError: new RateLimitedError() });
  await withServer({ storage: limited }, async (baseUrl) => {
    assert.equal((await postPilot(baseUrl)).status, 429);
  });

  const unavailable = recordingStorage({
    saveError: new StorageUnavailableError(new Error("pool exhausted")),
  });
  await withServer({ storage: unavailable }, async (baseUrl) => {
    assert.equal((await postPilot(baseUrl)).status, 503);
  });
});

test("static serving refuses traversal and private directories", async () => {
  await withServer({ storage: recordingStorage() }, async (baseUrl) => {
    const home = await fetch(`${baseUrl}/`);
    assert.equal(home.status, 200);
    assert.match(await home.text(), /Haechi/);

    for (const path of [
      "/db/001-pilot-requests.sql",
      "/tests/server.test.mjs",
      "/runtime/pilot-requests.ndjson",
    ]) {
      const response = await fetch(`${baseUrl}${path}`);
      assert.equal(response.status, 404, `${path} must not be served`);
    }

    const malformed = await fetch(`${baseUrl}/%E0%A4%A`);
    assert.equal(malformed.status, 400);
  });
});
