import {
  MAX_BODY_BYTES,
  generateReference,
  hashIp,
  jsonResponse,
  validatePilotRequest,
} from "../_lib.js";

const RATE_LIMIT = 5;
const RATE_WINDOW_MS = 60 * 60 * 1000;

// POST /api/pilot
export async function onRequestPost({ request, env }) {
  const contentType = request.headers.get("content-type") || "";
  if (
    contentType.split(";", 1)[0].trim().toLowerCase() !== "application/json"
  ) {
    return jsonResponse(415, {
      error: "Content-Type must be application/json.",
    });
  }
  const declaredLength = Number(request.headers.get("content-length") || "0");
  if (
    !Number.isFinite(declaredLength) ||
    declaredLength < 0 ||
    declaredLength > MAX_BODY_BYTES
  ) {
    return jsonResponse(413, { error: "Request body is too large." });
  }

  let body;
  try {
    const raw = await request.text();
    if (new TextEncoder().encode(raw).byteLength > MAX_BODY_BYTES) {
      return jsonResponse(413, { error: "Request body is too large." });
    }
    body = JSON.parse(raw || "{}");
  } catch {
    return jsonResponse(400, { error: "Request body must be valid JSON." });
  }

  const { request: pilot, errors } = validatePilotRequest(body);
  if (Object.keys(errors).length) {
    return jsonResponse(422, {
      error: "Please correct the highlighted fields.",
      fields: errors,
    });
  }

  const db = env.PILOT_DB;
  if (!db) {
    // Fail closed rather than silently dropping an enterprise lead.
    return jsonResponse(503, {
      error:
        "Submissions are temporarily unavailable. Please try again shortly.",
    });
  }

  const clientIp = request.headers.get("CF-Connecting-IP");
  if (
    !clientIp ||
    typeof env.PILOT_IP_SALT !== "string" ||
    env.PILOT_IP_SALT.length < 32
  ) {
    return jsonResponse(503, {
      error:
        "Submissions are temporarily unavailable. Please try again shortly.",
    });
  }
  let ipHash;
  try {
    ipHash = await hashIp(clientIp, env.PILOT_IP_SALT);
  } catch {
    return jsonResponse(503, {
      error:
        "Submissions are temporarily unavailable. Please try again shortly.",
    });
  }

  const since = new Date(Date.now() - RATE_WINDOW_MS).toISOString();
  const reference = generateReference();
  const receivedAt = new Date().toISOString();
  const userAgent = (request.headers.get("user-agent") || "").slice(0, 240);

  try {
    const result = await db
      .prepare(
        `INSERT INTO pilot_requests
           (reference, received_at, organisation, work_email, role, deployment, message, user_agent, ip_hash)
         SELECT ?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9
         WHERE (
           SELECT COUNT(*) FROM pilot_requests WHERE ip_hash = ?9 AND received_at > ?10
         ) < ?11`,
      )
      .bind(
        reference,
        receivedAt,
        pilot.organisation,
        pilot.workEmail,
        pilot.role,
        pilot.deployment,
        pilot.message,
        userAgent,
        ipHash,
        since,
        RATE_LIMIT,
      )
      .run();
    if (result?.meta?.changes !== 1) {
      return jsonResponse(429, {
        error: "Too many requests. Please try again later.",
      });
    }
  } catch {
    return jsonResponse(503, {
      error:
        "Submissions are temporarily unavailable. Please try again shortly.",
    });
  }

  // Optional fire-and-forget notification (Slack/CRM webhook). Never blocks the response.
  if (env.PILOT_WEBHOOK_URL) {
    try {
      await fetch(env.PILOT_WEBHOOK_URL, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          text: `New Haechi pilot request ${reference}: ${pilot.organisation} (${pilot.deployment})`,
        }),
      });
    } catch (error) {
      console.error(
        "Pilot notification failed.",
        error instanceof Error ? error.name : "UnknownError",
      );
    }
  }

  return jsonResponse(201, { ok: true, reference });
}
