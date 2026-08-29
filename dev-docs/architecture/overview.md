# Haechi Architecture: Single Source of Truth

Haechi is an **integration platform**, not a from-scratch build (Integration
Document §1, Engineering Principle #2: _adapters over deep forks_). This document
is the canonical map. If code and this map disagree, that is a bug in one of them.

## Design stance: ports & adapters (hexagonal)

The **Haechi-owned domain core** (§1.2) contains only differentiated logic and
depends on the standard library (ADR-003). Every dependency on a proven upstream
project is expressed as a **port** (a Go interface owned by the core) with:

- a **dev adapter**: in-memory / loopback / local, used for tests and laptops;
- a **prod adapter**: wires the actual upstream through its stable API/SDK.

The core never imports an upstream directly. Swapping an adapter never changes
core behaviour. Upstreams are **pinned in `third_party/manifest.yaml`** and
consumed as external services or images, never vendored (§5).

```
            +-------------------- Haechi-owned core (stdlib only) --------------------+
   admin →  |  control-api    capability-issuer/verifier   route-orchestrator   gim   |
   agent →  |  agent/core     pkg/capability                internal/model/signing    |
            +----------┬-------------┬---------------┬----------------┬----------------+
                  port │        port │          port │           port │      ... ports
        ┌──────────────▼──┐  ┌───────▼───────┐  ┌────▼─────┐  ┌────────▼────────┐
  dev   │ MemoryStore     │  │ StaticEngine  │  │ Memory   │  │ in-proc Ed25519 │
        │ Loopback tunnel │  │ MemoryChecker │  │ Bus      │  │ Collecting sink │
        ├─────────────────┤  ├───────────────┤  ├──────────┤  ├─────────────────┤
  prod  │ Postgres        │  │ OPA (REST)    │  │ NATS     │  │ OpenBao Transit │
        │ wireguard-go/   │  │ Keycloak OIDC │  │ subjects │  │ OTel Collector  │
        │ NetBird         │  │               │  │          │  │                 │
        └─────────────────┘  └───────────────┘  └──────────┘  └─────────────────┘
```

## Component ↔ upstream ↔ port ↔ adapter ↔ contract ↔ pin

| Haechi core (owns)     | Upstream (integrates)              | Port (interface)                            | Dev adapter                      | Prod adapter                             | Contract                                                | Manifest pin                                  |
| ---------------------- | ---------------------------------- | ------------------------------------------- | -------------------------------- | ---------------------------------------- | ------------------------------------------------------- | --------------------------------------------- |
| `control-api` state    | PostgreSQL                         | `controlapi.Store`                          | `MemoryStore`                    | `adapters/postgres`                      | `api/openapi/control-api.yaml`, `deploy/.../schema.sql` | postgres 16.4                                 |
| `control-api` authz    | Open Policy Agent                  | `controlapi.PolicyEngine`                   | `StaticEngine` (deny-by-default) | `adapters/opa` (REST `/v1/data`)         | `deploy/opa/policy/*.rego`                              | opa 0.70.0                                    |
| `entitlement` identity | Keycloak (OIDC)                    | `entitlement.Checker` / `identity.Verifier` | `MemoryChecker`                  | `adapters/keycloak` (discovery+JWKS+JWT) | `api/openapi/control-api.yaml`                          | keycloak 25.0.6                               |
| all signers            | OpenBao (Transit)                  | `signing.Signer`                            | in-proc `PrivateKey`             | `adapters/openbao` (Transit sign)        | ADR-005                                                 | openbao 2.0.1                                 |
| control events         | NATS                               | `events.Publisher`                          | `events.MemoryBus`               | `adapters/nats`                          | `api/events/*.json`                                     | nats 2.10.20                                  |
| `agent` tunnel         | wireguard-go / NetBird / Rosenpass | `tunnel.Tunnel`                             | `Loopback`                       | `adapters/wireguard` (UAPI)              | ADR-003, ADR-005                                        | wireguard-go, netbird 0.28.9, rosenpass 0.2.2 |
| `gim` sink             | OpenTelemetry Collector            | `gim.Sink`                                  | `CollectingSink`                 | `adapters/otel`                          | telemetry-prohibition-list.md                           | otel-collector 0.109.0                        |
| capability crypto      | RFC 9474 (research: CIRCL)         | none (owned)                                | none                             | none                                     | `pkg/capability` + ADR-004                              | circl 1.4.0 (gated)                           |

## Trust-zone placement (Integration Document §4.1)

| Plane               | Components                                          | Upstreams                           |
| ------------------- | --------------------------------------------------- | ----------------------------------- |
| Identity            | `entitlement`                                       | Keycloak                            |
| Capability issuance | `capability-issuer`                                 | OpenBao (keys)                      |
| Data plane          | `gateway-controller`, `capability-verifier`, tunnel | wireguard-go/NetBird, Xray (opt-in) |
| Control plane       | `control-api`, `route-orchestrator`                 | Postgres, NATS, OPA, OpenBao        |
| Analytics           | `gim`                                               | OTel Collector, ClickHouse          |

The **capability** flows across zones so that the issuer cannot link to the
gateway (blind signatures, ADR-004) and the gateway never sees identity.

## Why these tradeoffs

- **stdlib-only core, upstreams behind ports** keeps the fail-closed trusted
  computing base tiny while still standing on mature projects for everything
  non-differentiated. Cost: an adapter per upstream. Benefit: upstream churn,
  CVEs and forks are isolated to one small package each (Risk table §15 row 1).
- **OPA for authz** instead of bespoke rules: one policy language for devices,
  admins, routes and features; deny-by-default is expressible and testable. The
  `StaticEngine` mirrors the same decision shape for offline/dev.
- **Keycloak for identity** (federation/MFA/SAML/OIDC) instead of building auth;
  the gateway never receives the Keycloak token (zone separation).
- **OpenBao Transit for signing** so private keys never live in app pods (§13
  key management); the in-proc signer exists only for tests.
- **Blind-signature capability owned by Haechi** because no upstream provides the
  exact unlinkable, audience-bound admission token the product needs (§1.2).

## Adapter directory

`adapters/<upstream>/` holds one prod adapter per upstream. Each is small, is the
only place that imports that upstream, names its pinned version, and has a
contract test (httptest for REST upstreams) proving the wire integration.
