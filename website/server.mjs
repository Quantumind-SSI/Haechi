// Haechi website origin server.
//
// Deployment shapes:
//   local development     static files + file storage, same-origin form post
//   Azure Container Apps  API origin backed by Azure Database for PostgreSQL,
//                         serving cross-origin requests from Cloudflare Pages
//
// Storage is injected, so this module never branches on deployment target.
// Every failure path denies the submission rather than dropping it silently.

import { createServer } from "node:http";
import { createReadStream } from "node:fs";
import { readFile, stat } from "node:fs/promises";
import { dirname, extname, join, normalize, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { createHmac, randomBytes, timingSafeEqual } from "node:crypto";
import {
  RateLimitedError,
  StorageUnavailableError,
  createFileStorage,
  createPostgresStorage,
  openPostgresPool,
} from "./storage.mjs";

const siteRoot = dirname(fileURLToPath(import.meta.url));
const defaultDataDir = join(siteRoot, "runtime");
const maximumBodyBytes = 16 * 1024;
const minimumSaltLength = 32;

const contentTypes = new Map([
  [".css", "text/css; charset=utf-8"],
  [".html", "text/html; charset=utf-8"],
  [".ico", "image/x-icon"],
  [".js", "text/javascript; charset=utf-8"],
  [".json", "application/json; charset=utf-8"],
  [".svg", "image/svg+xml; charset=utf-8"],
  [".txt", "text/plain; charset=utf-8"],
  [".webmanifest", "application/manifest+json; charset=utf-8"],
]);

const publicRoutes = new Map([
  ["/", "index.html"],
  ["/index.html", "index.html"],
  ["/privacy", "privacy.html"],
  ["/privacy.html", "privacy.html"],
  ["/terms", "terms.html"],
  ["/terms.html", "terms.html"],
]);

const prohibitedRoots = new Set(["runtime", "tests", "db", "node_modules"]);

function applySecurityHeaders(response, connectSources) {
  const connectSrc = ["'self'", ...connectSources].join(" ");
  response.setHeader(
    "Content-Security-Policy",
    `default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src ${connectSrc}; font-src 'self'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'; object-src 'none'`,
  );
  response.setHeader("Cross-Origin-Opener-Policy", "same-origin");
  response.setHeader("Cross-Origin-Resource-Policy", "same-origin");
  response.setHeader(
    "Permissions-Policy",
    "camera=(), microphone=(), geolocation=(), payment=(), usb=()",
  );
  response.setHeader("Referrer-Policy", "strict-origin-when-cross-origin");
  response.setHeader("X-Content-Type-Options", "nosniff");
  response.setHeader("X-Frame-Options", "DENY");
}

function json(response, statusCode, payload) {
  response.writeHead(statusCode, {
    "Content-Type": "application/json; charset=utf-8",
    "Cache-Control": "no-store",
  });
  response.end(JSON.stringify(payload));
}

async function parseJsonBody(request) {
  let raw = "";
  for await (const chunk of request) {
    raw += chunk;
    if (Buffer.byteLength(raw) > maximumBodyBytes) {
      const error = new Error("Request body is too large.");
      error.statusCode = 413;
      throw error;
    }
  }

  try {
    return JSON.parse(raw || "{}");
  } catch {
    const error = new Error("Request body must be valid JSON.");
    error.statusCode = 400;
    throw error;
  }
}

export function cleanText(value, maximumLength) {
  if (typeof value !== "string") return "";
  return value
    .trim()
    .replace(/[\u0000-\u001f\u007f]/g, " ")
    .slice(0, maximumLength);
}

export function validatePilotRequest(body) {
  const request = {
    organisation: cleanText(body.organisation, 120),
    workEmail: cleanText(body.workEmail, 180).toLowerCase(),
    role: cleanText(body.role, 100),
    deployment: cleanText(body.deployment, 40),
    message: cleanText(body.message, 1000),
    consent: body.consent === true,
    website: cleanText(body.website, 200),
  };

  const allowedDeployments = new Set([
    "saas",
    "dedicated",
    "restricted",
    "undecided",
  ]);
  const emailPattern = /^[^\s@]+@[^\s@]+\.[^\s@]{2,}$/;
  const errors = {};

  if (request.organisation.length < 2)
    errors.organisation = "Enter your organisation name.";
  if (!emailPattern.test(request.workEmail))
    errors.workEmail = "Enter a valid work email address.";
  if (!allowedDeployments.has(request.deployment))
    errors.deployment = "Choose a deployment preference.";
  if (!request.consent)
    errors.consent = "Consent is required so we can respond.";
  if (request.website) errors.website = "Automated submission rejected.";

  return { request, errors };
}

/** Keyed pseudonym for a client address. The raw address is never stored. */
export function hashIp(ip, salt) {
  return createHmac("sha256", salt).update(ip).digest("hex");
}

/**
 * Resolves the client address.
 *
 * X-Forwarded-For is only honoured when the deployment explicitly trusts its
 * ingress. Trusting it unconditionally would let any caller forge the header
 * and bypass rate limiting entirely.
 */
export function clientAddress(request, trustProxy) {
  if (trustProxy) {
    const forwarded = request.headers["x-forwarded-for"];
    if (typeof forwarded === "string" && forwarded.length > 0) {
      const first = forwarded.split(",")[0].trim();
      if (first) return first;
    }
  }
  return request.socket.remoteAddress || "unknown";
}

function originAllowed(origin, allowedOrigins) {
  if (typeof origin !== "string" || origin === "") return false;
  const candidate = Buffer.from(origin);
  return allowedOrigins.some((allowed) => {
    const expected = Buffer.from(allowed);
    return (
      expected.length === candidate.length &&
      timingSafeEqual(expected, candidate)
    );
  });
}

function applyCorsHeaders(response, origin) {
  response.setHeader("Access-Control-Allow-Origin", origin);
  response.setHeader("Access-Control-Allow-Methods", "POST, OPTIONS");
  response.setHeader("Access-Control-Allow-Headers", "Content-Type");
  response.setHeader("Access-Control-Max-Age", "600");
}

/**
 * Builds the HTTP server.
 *
 * @param {object} options
 * @param {object} options.storage    storage adapter, defaults to file storage
 * @param {string[]} options.allowedOrigins  origins permitted to post the form
 * @param {string} options.ipSalt     HMAC key for client pseudonyms
 * @param {boolean} options.trustProxy honour X-Forwarded-For from the ingress
 */
export function createHaechiServer(options = {}) {
  const dataDir =
    options.dataDir || process.env.HAECHI_SITE_DATA_DIR || defaultDataDir;
  const storage = options.storage || createFileStorage({ dataDir });
  const allowedOrigins = options.allowedOrigins || [];
  const trustProxy = options.trustProxy === true;
  const ipSalt = options.ipSalt || randomBytes(32).toString("hex");

  if (ipSalt.length < minimumSaltLength) {
    throw new Error(
      `ipSalt must be at least ${minimumSaltLength} characters to pseudonymise client addresses`,
    );
  }

  const server = createServer(async (request, response) => {
    applySecurityHeaders(response, allowedOrigins);

    const origin = request.headers.origin;
    const corsPermitted = originAllowed(origin, allowedOrigins);
    if (corsPermitted) {
      response.setHeader("Vary", "Origin");
      applyCorsHeaders(response, origin);
    }

    const requestUrl = new URL(request.url || "/", "http://localhost");

    if (request.method === "OPTIONS") {
      if (!corsPermitted) {
        json(response, 403, { error: "Origin is not permitted." });
        return;
      }
      response.writeHead(204);
      response.end();
      return;
    }

    if (request.method === "GET" && requestUrl.pathname === "/api/health") {
      try {
        await storage.ping();
      } catch {
        json(response, 503, {
          status: "unavailable",
          service: "haechi-website",
        });
        return;
      }
      json(response, 200, { status: "ok", service: "haechi-website" });
      return;
    }

    if (request.method === "POST" && requestUrl.pathname === "/api/pilot") {
      if (origin !== undefined && !corsPermitted) {
        json(response, 403, { error: "Origin is not permitted." });
        return;
      }

      const contentType = request.headers["content-type"] || "";
      if (
        contentType.split(";", 1)[0].trim().toLowerCase() !== "application/json"
      ) {
        json(response, 415, {
          error: "Content-Type must be application/json.",
        });
        return;
      }

      try {
        const body = await parseJsonBody(request);
        const { request: pilotRequest, errors } = validatePilotRequest(body);
        if (Object.keys(errors).length) {
          json(response, 422, {
            error: "Please correct the highlighted fields.",
            fields: errors,
          });
          return;
        }

        const ipHash = hashIp(clientAddress(request, trustProxy), ipSalt);
        const { reference } = await storage.save({
          request: pilotRequest,
          userAgent: cleanText(request.headers["user-agent"], 240),
          ipHash,
        });
        json(response, 201, { ok: true, reference });
      } catch (error) {
        if (error instanceof RateLimitedError) {
          json(response, 429, {
            error: "Too many requests. Please try again later.",
          });
          return;
        }
        if (error instanceof StorageUnavailableError) {
          json(response, 503, {
            error:
              "Submissions are temporarily unavailable. Please try again shortly.",
          });
          return;
        }
        json(response, error.statusCode || 500, {
          error: error.statusCode
            ? error.message
            : "Unable to save the request.",
        });
      }
      return;
    }

    if (request.method !== "GET" && request.method !== "HEAD") {
      json(response, 405, { error: "Method not allowed." });
      return;
    }

    await serveStatic(requestUrl.pathname, response);
  });

  server.on("close", () => {
    storage.close().catch(() => {});
  });
  return server;
}

async function serveStatic(requestPath, response) {
  const routeFile = publicRoutes.get(requestPath);
  let relativePath;
  if (routeFile) {
    relativePath = routeFile;
  } else {
    let decoded;
    try {
      decoded = decodeURIComponent(requestPath);
    } catch {
      json(response, 400, { error: "Malformed path." });
      return;
    }
    relativePath = normalize(decoded).replace(/^[/\\]+/, "");
  }
  const firstSegment = relativePath.split(/[\\/]/)[0];

  if (!relativePath || prohibitedRoots.has(firstSegment)) {
    json(response, 404, { error: "Not found." });
    return;
  }

  const absolutePath = resolve(siteRoot, relativePath);
  if (!absolutePath.startsWith(`${resolve(siteRoot)}/`)) {
    json(response, 404, { error: "Not found." });
    return;
  }

  try {
    const fileStat = await stat(absolutePath);
    if (!fileStat.isFile()) throw new Error("Not a file");
    const extension = extname(absolutePath).toLowerCase();
    const contentType = contentTypes.get(extension);
    if (!contentType) {
      json(response, 404, { error: "Not found." });
      return;
    }

    response.writeHead(200, {
      "Content-Type": contentType,
      "Content-Length": fileStat.size,
      "Cache-Control":
        extension === ".html" ? "no-cache" : "public, max-age=3600",
    });
    createReadStream(absolutePath).pipe(response);
  } catch {
    const notFound = await readFile(join(siteRoot, "index.html"));
    response.writeHead(404, { "Content-Type": "text/html; charset=utf-8" });
    response.end(notFound);
  }
}

/**
 * Selects storage from the environment.
 *
 * HAECHI_DATABASE_URL selects PostgreSQL and makes the pseudonym key
 * mandatory, because hashes are persisted and shared across instances. Without
 * the variable the server uses local file storage for development.
 */
async function storageFromEnvironment() {
  const connectionString = process.env.HAECHI_DATABASE_URL;
  if (!connectionString) {
    return {
      storage: createFileStorage({
        dataDir: process.env.HAECHI_SITE_DATA_DIR || defaultDataDir,
      }),
      requiresSalt: false,
    };
  }
  const pool = await openPostgresPool({ connectionString });
  return { storage: createPostgresStorage({ pool }), requiresSalt: true };
}

function parseAllowedOrigins(value) {
  if (typeof value !== "string" || value.trim() === "") return [];
  return value
    .split(",")
    .map((origin) => origin.trim())
    .filter((origin) => origin !== "");
}

async function main() {
  const port = Number.parseInt(process.env.HAECHI_SITE_PORT || "4173", 10);
  const host = process.env.HAECHI_SITE_HOST || "127.0.0.1";
  const { storage, requiresSalt } = await storageFromEnvironment();
  const ipSalt = process.env.HAECHI_PILOT_IP_SALT;

  if (requiresSalt && (!ipSalt || ipSalt.length < minimumSaltLength)) {
    throw new Error(
      `HAECHI_PILOT_IP_SALT of at least ${minimumSaltLength} characters is required when HAECHI_DATABASE_URL is set`,
    );
  }

  await storage.ping();

  const server = createHaechiServer({
    storage,
    ipSalt,
    allowedOrigins: parseAllowedOrigins(process.env.HAECHI_ALLOWED_ORIGINS),
    trustProxy: process.env.HAECHI_TRUST_PROXY === "true",
  });

  server.listen(port, host, () => {
    process.stdout.write(
      `Haechi website listening on http://${host}:${port} (storage: ${storage.kind})\n`,
    );
  });

  // Container Apps sends SIGTERM before replacing a revision.
  for (const signal of ["SIGTERM", "SIGINT"]) {
    process.on(signal, () => {
      server.close(() => process.exit(0));
      setTimeout(() => process.exit(1), 10_000).unref();
    });
  }
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(process.argv[1]).href
) {
  main().catch((error) => {
    process.stderr.write(`Haechi website failed to start: ${error.message}\n`);
    process.exit(1);
  });
}
