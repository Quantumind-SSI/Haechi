# Haechi website

A dependency-free Node.js website for the Haechi enterprise secure communications platform.

## Run locally

```bash
npm start
```

Open `http://127.0.0.1:4173`.

## Test

```bash
npm test
```

## Pilot requests

The pilot form posts to `POST /api/pilot`. In this reference deployment, validated requests are appended to `runtime/pilot-requests.ndjson` with file mode `0600`; the runtime directory is excluded from Git. Before a public deployment, replace this file-backed adapter with an approved CRM or encrypted data store, add an organisation-controlled notification workflow, and complete the privacy impact assessment.

Environment variables:

- `HAECHI_SITE_PORT`: listening port, default `4173`
- `HAECHI_SITE_HOST`: listening interface, default `127.0.0.1`
- `HAECHI_SITE_DATA_DIR`: pilot-request storage directory

TLS should terminate at the production ingress or reverse proxy. The server supplies a restrictive content security policy and other browser security headers.
