// Shared helpers for Haechi Cloudflare Pages Functions.
//
// This module is imported by the Functions (Workers runtime) AND by
// scripts/pages-build.mjs (Node) to keep a single source of truth for the
// security headers. Keep the top level free of runtime-only globals so Node can
// import it safely; `crypto`/`TextEncoder` are only referenced inside functions.

export const SECURITY_HEADERS = {
  "Content-Security-Policy":
    "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'; font-src 'self'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'; object-src 'none'",
  "Cross-Origin-Opener-Policy": "same-origin",
  "Cross-Origin-Resource-Policy": "same-origin",
  "Permissions-Policy":
    "camera=(), microphone=(), geolocation=(), payment=(), usb=()",
  "Referrer-Policy": "strict-origin-when-cross-origin",
  // Edge is HTTPS on Cloudflare, so we can assert HSTS (the Node origin could not).
  "Strict-Transport-Security": "max-age=31536000; includeSubDomains",
  "X-Content-Type-Options": "nosniff",
  "X-Frame-Options": "DENY",
};

export const MAX_BODY_BYTES = 16 * 1024;

const ALLOWED_DEPLOYMENTS = new Set([
  "saas",
  "dedicated",
  "restricted",
  "undecided",
]);
const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]{2,}$/;

export function jsonResponse(status, payload, extraHeaders = {}) {
  return new Response(JSON.stringify(payload), {
    status,
    headers: {
      "Content-Type": "application/json; charset=utf-8",
      // _headers does not apply to Functions responses, so set them here too.
      ...SECURITY_HEADERS,
      ...extraHeaders,
    },
  });
}

export function cleanText(value, maximumLength) {
  if (typeof value !== "string") return "";
  return value
    .trim()
    .replace(/[\u0000-\u001f\u007f]/g, " ")
    .slice(0, maximumLength);
}

// Mirrors server.mjs validatePilotRequest so the frontend contract is unchanged.
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

  const errors = {};
  if (request.organisation.length < 2)
    errors.organisation = "Enter your organisation name.";
  if (!EMAIL_PATTERN.test(request.workEmail))
    errors.workEmail = "Enter a valid work email address.";
  if (!ALLOWED_DEPLOYMENTS.has(request.deployment))
    errors.deployment = "Choose a deployment preference.";
  if (!request.consent)
    errors.consent = "Consent is required so we can respond.";
  if (request.website) errors.website = "Automated submission rejected.";

  return { request, errors };
}

export function generateReference() {
  return `HCH-${crypto.randomUUID().slice(0, 8).toUpperCase()}`;
}

// Produce a keyed pseudonymous identifier without persisting the client IP.
export async function hashIp(ip, salt) {
  if (
    typeof ip !== "string" ||
    !ip ||
    typeof salt !== "string" ||
    salt.length < 32
  ) {
    throw new Error(
      "IP hashing requires an address and a secret of at least 32 characters.",
    );
  }
  const encoder = new TextEncoder();
  const key = await crypto.subtle.importKey(
    "raw",
    encoder.encode(salt),
    { name: "HMAC", hash: "SHA-256" },
    false,
    ["sign"],
  );
  const digest = await crypto.subtle.sign("HMAC", key, encoder.encode(ip));
  return Array.from(new Uint8Array(digest))
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");
}
