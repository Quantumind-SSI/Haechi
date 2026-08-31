# Haechi website

The public Haechi website and the pilot-request API.

## Deployment topology

Cloudflare Pages serves the static assets. The pilot API and all submitted data
live in Azure Container Apps backed by Azure Database for PostgreSQL in UK
South. Cloudflare stores nothing.

The two sides are coupled by one value, the API base URL, which is baked into
`config.js` and the `connect-src` directive at build time. See
[`../dev-docs/operations/deployment.md`](../dev-docs/operations/deployment.md).

## Run locally

```bash
npm start
```

Open `http://127.0.0.1:4173`. Without `HAECHI_DATABASE_URL` the server uses
file storage and serves the form same-origin, so no Azure resources are needed.

## Test

```bash
npm test
npm run test:responsive
```

The PostgreSQL adapter tests are skipped unless a disposable database is
available. To run them:

```bash
export HAECHI_TEST_DATABASE_URL="postgresql://postgres@127.0.0.1:5432/haechi_pilot"
npm test
```

The responsive interaction audit launches headless Chrome at phone, tablet,
short-landscape and desktop viewports. Set `CHROME_PATH` when Chrome or Chromium
is not installed in one of the standard macOS or Linux locations.

## Build

| Command               | Output                                                   | Used by             |
| --------------------- | -------------------------------------------------------- | ------------------- |
| `npm run build`       | `dist/`, the self-contained Node origin                  | Container image     |
| `npm run pages:build` | `public/`, static assets plus `config.js` and `_headers` | Cloudflare Pages    |
| `npm run migrate`     | applies `db/*.sql`                                       | Azure migration job |

Both output directories are generated and excluded from Git.

## Pilot requests

The form posts to `POST /api/pilot`. Requests are validated, rate limited to
five per hour per caller, and stored.

Client IP addresses are never stored. They are reduced to a keyed HMAC used
only for rate limiting. Consent is required to submit but is not stored, and
the honeypot field is never stored.

In PostgreSQL the rate-limit check and the insert are a single statement, so
concurrent submissions cannot exceed the limit.

## Configuration

| Variable                 | Default     | Purpose                                                                       |
| ------------------------ | ----------- | ----------------------------------------------------------------------------- |
| `HAECHI_SITE_PORT`       | `4173`      | Listening port                                                                |
| `HAECHI_SITE_HOST`       | `127.0.0.1` | Listening interface. Containers must set `0.0.0.0`                            |
| `HAECHI_SITE_DATA_DIR`   | `runtime/`  | File-storage directory, local development only                                |
| `HAECHI_DATABASE_URL`    | unset       | Selects PostgreSQL storage. Requires TLS                                      |
| `HAECHI_PILOT_IP_SALT`   | ephemeral   | HMAC key. Mandatory, minimum 32 characters, when `HAECHI_DATABASE_URL` is set |
| `HAECHI_ALLOWED_ORIGINS` | none        | Comma-separated origins permitted to post the form                            |
| `HAECHI_TRUST_PROXY`     | `false`     | Honour `X-Forwarded-For`. Enable only behind a trusted ingress                |
| `HAECHI_API_BASE`        | none        | Build-time only. API origin baked into the Cloudflare output                  |

`HAECHI_TRUST_PROXY` must stay `false` unless an ingress you control overwrites
`X-Forwarded-For`. Trusting it on a directly exposed server would let any caller
forge the header and bypass rate limiting.

When `HAECHI_DATABASE_URL` is set the server refuses to start without a
pseudonym key, because hashes are persisted and shared across replicas.

## Security

The server sets a restrictive content security policy and the usual browser
hardening headers, and refuses requests that are not `application/json`.
Readiness reports unhealthy when storage is unreachable, so a broken revision
never receives traffic. TLS terminates at the Cloudflare edge and at the Azure
Container Apps ingress.
