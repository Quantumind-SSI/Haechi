import { createServer } from "node:http";
import { appendFile, chmod, mkdir, readFile, stat } from "node:fs/promises";
import { createReadStream } from "node:fs";
import { dirname, extname, join, normalize, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { randomUUID } from "node:crypto";

const siteRoot = dirname(fileURLToPath(import.meta.url));
const defaultDataDir = join(siteRoot, "runtime");
const maximumBodyBytes = 16 * 1024;

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

const prohibitedRoots = new Set(["runtime", "tests"]);

function applySecurityHeaders(response) {
  response.setHeader(
    "Content-Security-Policy",
    "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'; font-src 'self'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'; object-src 'none'",
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

function cleanText(value, maximumLength) {
  if (typeof value !== "string") return "";
  return value
    .trim()
    .replace(/[\u0000-\u001f\u007f]/g, " ")
    .slice(0, maximumLength);
}

function validatePilotRequest(body) {
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

function createRateLimiter({
  maximum = 5,
  windowMs = 60 * 60 * 1000,
  maximumClients = 10_000,
} = {}) {
  const clients = new Map();

  return function allow(key) {
    const now = Date.now();
    const existing = clients.get(key);
    if (!existing || existing.resetAt <= now) {
      if (!existing && clients.size >= maximumClients) {
        for (const [client, limit] of clients) {
          if (limit.resetAt <= now) clients.delete(client);
        }
        if (clients.size >= maximumClients) return false;
      }
      clients.set(key, { count: 1, resetAt: now + windowMs });
      return true;
    }
    if (existing.count >= maximum) return false;
    existing.count += 1;
    return true;
  };
}

async function savePilotRequest(dataDir, request, metadata) {
  await mkdir(dataDir, { recursive: true, mode: 0o700 });
  const outputPath = join(dataDir, "pilot-requests.ndjson");
  const record = {
    id: `HCH-${randomUUID().slice(0, 8).toUpperCase()}`,
    receivedAt: new Date().toISOString(),
    ...request,
    consent: undefined,
    website: undefined,
    metadata,
  };

  await appendFile(outputPath, `${JSON.stringify(record)}\n`, {
    encoding: "utf8",
    mode: 0o600,
  });
  await chmod(outputPath, 0o600);
  return record.id;
}

async function serveStatic(requestPath, response) {
  const routeFile = publicRoutes.get(requestPath);
  const relativePath =
    routeFile ||
    normalize(decodeURIComponent(requestPath)).replace(/^[/\\]+/, "");
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

export function createHaechiServer(options = {}) {
  const dataDir =
    options.dataDir || process.env.HAECHI_SITE_DATA_DIR || defaultDataDir;
  const allowRequest = createRateLimiter(options.rateLimit);

  return createServer(async (request, response) => {
    applySecurityHeaders(response);
    const requestUrl = new URL(request.url || "/", "http://localhost");

    if (request.method === "GET" && requestUrl.pathname === "/api/health") {
      json(response, 200, { status: "ok", service: "haechi-website" });
      return;
    }

    if (request.method === "POST" && requestUrl.pathname === "/api/pilot") {
      const contentType = request.headers["content-type"] || "";
      if (
        contentType.split(";", 1)[0].trim().toLowerCase() !== "application/json"
      ) {
        json(response, 415, {
          error: "Content-Type must be application/json.",
        });
        return;
      }
      const clientAddress = request.socket.remoteAddress || "unknown";
      if (!allowRequest(clientAddress)) {
        json(response, 429, {
          error: "Too many requests. Please try again later.",
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

        const reference = await savePilotRequest(dataDir, pilotRequest, {
          userAgent: cleanText(request.headers["user-agent"], 240),
        });
        json(response, 201, { ok: true, reference });
      } catch (error) {
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
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(process.argv[1]).href
) {
  const port = Number.parseInt(process.env.HAECHI_SITE_PORT || "4173", 10);
  const host = process.env.HAECHI_SITE_HOST || "127.0.0.1";
  const server = createHaechiServer();
  server.listen(port, host, () => {
    process.stdout.write(
      `Haechi website listening on http://${host}:${port}\n`,
    );
  });
}
