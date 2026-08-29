import assert from "node:assert/strict";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createHaechiServer } from "../server.mjs";

const dataDir = await mkdtemp(join(tmpdir(), "haechi-site-test-"));
const server = createHaechiServer({ dataDir });

try {
  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  const address = server.address();
  const baseUrl = `http://127.0.0.1:${address.port}`;

  const home = await fetch(baseUrl);
  assert.equal(home.status, 200);
  assert.match(await home.text(), /Haechi/);

  const health = await fetch(`${baseUrl}/api/health`);
  assert.deepEqual(await health.json(), {
    status: "ok",
    service: "haechi-website",
  });

  const invalid = await fetch(`${baseUrl}/api/pilot`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({}),
  });
  assert.equal(invalid.status, 422);

  const wrongContentType = await fetch(`${baseUrl}/api/pilot`, {
    method: "POST",
    headers: { "Content-Type": "text/plain" },
    body: JSON.stringify({}),
  });
  assert.equal(wrongContentType.status, 415);

  const valid = await fetch(`${baseUrl}/api/pilot`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      organisation: "Example CNI Ltd",
      workEmail: "security@example.test",
      role: "CISO",
      deployment: "dedicated",
      message: "We are evaluating a controlled pilot.",
      consent: true,
      website: "",
    }),
  });
  assert.equal(valid.status, 201);
  const response = await valid.json();
  assert.match(response.reference, /^HCH-[A-F0-9]{8}$/);

  const stored = await readFile(join(dataDir, "pilot-requests.ndjson"), "utf8");
  assert.match(stored, /Example CNI Ltd/);
  assert.doesNotMatch(stored, /"consent"/);

  process.stdout.write("Haechi website smoke test passed.\n");
} finally {
  await new Promise((resolve) => server.close(resolve));
  await rm(dataDir, { recursive: true, force: true });
}
