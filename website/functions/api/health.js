import { jsonResponse } from "../_lib.js";

// GET /api/health
export function onRequestGet() {
  return jsonResponse(200, { status: "ok", service: "haechi-website" });
}
