import assert from "node:assert/strict";
import test from "node:test";
import { hashIp } from "../functions/_lib.js";
import { onRequestPost } from "../functions/api/pilot.js";

const VALID_BODY = {
  organisation: "Example CNI Ltd",
  workEmail: "security@example.test",
  role: "CISO",
  deployment: "dedicated",
  message: "Controlled pilot request.",
  consent: true,
  website: "",
};

test("IP pseudonyms require a strong secret and are deterministic", async () => {
  await assert.rejects(hashIp("203.0.113.1", "short"));
  const secret = "0123456789abcdef0123456789abcdef";
  const first = await hashIp("203.0.113.1", secret);
  const second = await hashIp("203.0.113.1", secret);
  const other = await hashIp("203.0.113.2", secret);
  assert.equal(first, second);
  assert.notEqual(first, other);
  assert.match(first, /^[a-f0-9]{64}$/);
});

test("pilot endpoint fails closed when the hashing secret is absent", async () => {
  const response = await onRequestPost({
    request: pilotRequest(),
    env: { PILOT_DB: {} },
  });
  assert.equal(response.status, 503);
});

test("pilot endpoint requires JSON content type", async () => {
  const request = new Request("https://haechi.example/api/pilot", {
    method: "POST",
    headers: {
      "CF-Connecting-IP": "203.0.113.1",
      "Content-Type": "text/plain",
    },
    body: JSON.stringify(VALID_BODY),
  });
  const response = await onRequestPost({ request, env: {} });
  assert.equal(response.status, 415);
});

test("pilot endpoint atomically enforces the database rate limit", async () => {
  let statement = "";
  const db = {
    prepare(sql) {
      statement = sql;
      return {
        bind() {
          return { run: async () => ({ meta: { changes: 0 } }) };
        },
      };
    },
  };
  const response = await onRequestPost({
    request: pilotRequest(),
    env: { PILOT_DB: db, PILOT_IP_SALT: "0123456789abcdef0123456789abcdef" },
  });
  assert.equal(response.status, 429);
  assert.match(statement, /INSERT INTO pilot_requests/);
  assert.match(statement, /SELECT COUNT\(\*\)/);
});

function pilotRequest() {
  return new Request("https://haechi.example/api/pilot", {
    method: "POST",
    headers: {
      "CF-Connecting-IP": "203.0.113.1",
      "Content-Type": "application/json",
    },
    body: JSON.stringify(VALID_BODY),
  });
}
