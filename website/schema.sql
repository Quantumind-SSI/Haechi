-- Haechi pilot request storage (Cloudflare D1 / SQLite).
-- Apply locally:  npx wrangler d1 execute haechi-pilot --local  --file=./schema.sql
-- Apply remote:   npx wrangler d1 execute haechi-pilot --remote --file=./schema.sql
--
-- Note: consent is required to submit but is not stored as a column (parity with the
-- original server, which stripped it). The honeypot `website` field is never stored.
-- Client IPs are stored only as a keyed HMAC (ip_hash) for rate limiting.

CREATE TABLE IF NOT EXISTS pilot_requests (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  reference     TEXT NOT NULL UNIQUE,
  received_at   TEXT NOT NULL,
  organisation  TEXT NOT NULL,
  work_email    TEXT NOT NULL,
  role          TEXT,
  deployment    TEXT NOT NULL,
  message       TEXT,
  user_agent    TEXT,
  ip_hash       TEXT
);

CREATE INDEX IF NOT EXISTS idx_pilot_requests_received_at ON pilot_requests (received_at);
CREATE INDEX IF NOT EXISTS idx_pilot_requests_ip_hash ON pilot_requests (ip_hash);
