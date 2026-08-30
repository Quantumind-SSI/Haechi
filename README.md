# Haechi

Haechi is an enterprise secure communications platform under development by Quantumind Ltd. The intended system combines a WireGuard-based network foundation with privacy-separated access control, post-quantum key reinforcement, controlled adaptive transports, and privacy-minimised operational intelligence.

## Engineering Status

This repository is an engineering prototype, not a production VPN or production control plane. The Go packages provide an executable in-process vertical slice and the website is an engineering preview. Real endpoint networking, durable control-plane composition, shared replay protection, complete service transport, and the RFC 9474 capability design remain release blockers.

See [`dev-docs/engineering/readiness.md`](dev-docs/engineering/readiness.md) for the evidence-based component status and production gates. All project documentation is indexed in [`dev-docs/README.md`](dev-docs/README.md).

## Repository Map

- `cmd/control-api/`: development control API composition root
- `services/`: control, entitlement, capability, route, gateway, and telemetry domain services
- `agent/`: endpoint coordination and simulated tunnel abstractions
- `adapters/`: external-system boundaries for Keycloak, OPA, OpenBao, PostgreSQL, NATS, OpenTelemetry, and WireGuard
- `api/`: OpenAPI, protobuf, and event contracts
- `deploy/`: local integration configuration and policy/schema assets
- `tests/integration/`: in-process vertical-slice tests
- `website/`: product website and pilot-request service
- `dev-docs/`: all project documentation, including architecture decisions, threat-model constraints, engineering readiness, and product, integration and brand material

## Verify The Repository

Requirements are Go 1.26, Node.js 20 or newer, npm 10 or newer, and Docker for container and OPA checks.

```bash
make web-install
make verify
make audit
```

`make ci` also runs the Docker-based OPA and container-build gates.

## Run The Website

```bash
npm --prefix website start
```

Open `http://127.0.0.1:4173`. Without a database configured the site uses local
file storage, so no cloud resources are required.

The website deploys as static assets on Cloudflare Pages with its API and data
on Azure Container Apps in UK South. See
[`dev-docs/operations/deployment.md`](dev-docs/operations/deployment.md) for the
runbook and [`website/README.md`](website/README.md) for configuration.

## Run The Development Control API

The executable refuses production mode because durable storage is not yet connected. Development mode still requires an explicit administrator token of at least 32 characters.

```bash
export HAECHI_ENV=development
export HAECHI_DEV_ADMIN_TOKEN="$(openssl rand -hex 32)"
go run ./cmd/control-api
```

Administrative requests require `Authorization: Bearer $HAECHI_DEV_ADMIN_TOKEN`. `/healthz` and proof-of-possession device enrollment are the only public routes.

## Security

Report vulnerabilities privately as described in [`SECURITY.md`](SECURITY.md). Security, anonymity, and post-quantum claims in planning material are not release claims until their corresponding gates are implemented, tested, and independently reviewed.
