// Single source of truth for the static site's browser security headers.
//
// The Node origin (server.mjs) sets the equivalent headers directly. This
// module exists so the Cloudflare Pages _headers file cannot drift from it.
//
// connect-src is widened to the Azure API origin when the form posts
// cross-origin. Passing no origin keeps the policy same-origin only.

export function securityHeaders({ apiOrigin = "" } = {}) {
  const connectSrc = ["'self'", ...(apiOrigin ? [apiOrigin] : [])].join(" ");
  return {
    "Content-Security-Policy": [
      "default-src 'self'",
      "img-src 'self' data:",
      "style-src 'self'",
      "script-src 'self'",
      `connect-src ${connectSrc}`,
      "font-src 'self'",
      "base-uri 'self'",
      "form-action 'self'",
      "frame-ancestors 'none'",
      "object-src 'none'",
    ].join("; "),
    "Cross-Origin-Opener-Policy": "same-origin",
    "Cross-Origin-Resource-Policy": "same-origin",
    "Permissions-Policy":
      "camera=(), microphone=(), geolocation=(), payment=(), usb=()",
    "Referrer-Policy": "strict-origin-when-cross-origin",
    // Cloudflare terminates TLS, so the static edge can assert HSTS.
    "Strict-Transport-Security": "max-age=31536000; includeSubDomains",
    "X-Content-Type-Options": "nosniff",
    "X-Frame-Options": "DENY",
  };
}
