# Engineering Readiness

Snapshot date: 2026-08-28

## Current Classification

Haechi is an engineering prototype with a working in-process vertical slice. It is not a production VPN, production control plane, or validated anonymous capability system.

The website and local development services are executable. The control-plane, entitlement, capability, route, gateway, and agent packages compile and their current automated tests pass. The only complete product flow uses in-memory and simulated adapters in `tests/integration/vertical_slice_test.go`.

## Component Status

| Component                    | State                        | Evidence                                                                                                                                         |
| ---------------------------- | ---------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| Website                      | Deployable, not yet deployed | 18 tests in CI including the PostgreSQL adapter against a real database; Bicep compiles and workflows lint, but no deployment has run end to end |
| Control API                  | Development only             | Runtime rejects non-development environments and requires bearer authentication                                                                  |
| PostgreSQL adapter           | Disconnected                 | Schema and adapter exist, but no production composition root opens the database                                                                  |
| Identity and policy adapters | Partial                      | Keycloak, OPA, and OpenBao adapters have isolated tests; no deployed end-to-end identity flow exists                                             |
| Endpoint agent               | Simulated                    | Signed artifact binding and fail-closed tunnel lifecycle are tested against a loopback tunnel                                                    |
| Gateway data plane           | Simulated                    | Capability admission exists; WireGuard peer programming and controlled egress do not                                                             |
| Capability protocol          | Research implementation      | Current blind RSA code is not the RFC 9474 construction selected by ADR-004                                                                      |
| Post-quantum reinforcement   | Simulated                    | No Rosenpass process integration or interoperability evidence exists                                                                             |
| Telemetry boundary           | Prototype                    | Whole-event rejection and typed value validation are tested; an executable export pipeline is absent                                             |

## Enforced Repository Gates

- Go formatting, module verification, vet, race-enabled tests, coverage generation, build, and `govulncheck`
- Website locked installs, formatting, migrations, tests against a real PostgreSQL service, both build outputs, and npm audit
- Cloudflare build asserts the API origin reaches both `config.js` and the content security policy
- OPA policy tests against the pinned OPA image
- Control API and website container builds
- Immutable GitHub Action commit references
- Automated dependency update proposals for Go, npm, Actions, and Docker
- Code ownership, contribution policy, private vulnerability reporting, and structured defect intake

## Production Release Blockers

1. Wire PostgreSQL into a production composition root with versioned migrations, backup and restore evidence, connection limits, deadlines, readiness checks, and tenant-isolation integration tests.
2. Define and enforce the administrator, device, and gateway authorization matrix. Add mutually authenticated service transport and bind device revocation to entitlement issuance.
3. Implement a real platform tunnel adapter and privileged helper. Prove firewall kill-switch behavior, DNS protection, reconnect teardown, route cleanup, and gateway failover on every supported operating system.
4. Replace the custom blind RSA construction with the reviewed RFC 9474 design selected by ADR-004. Add published vectors, key rotation, external cryptographic review, and a signed verifier directory.
5. Replace process-local replay state with an atomic shared store that fails closed, expires by epoch, and survives gateway restarts.
6. Add executable service entry points and authenticated transports for entitlement, capability issuance, routing, gateway admission, and telemetry.
7. Add integration environments that exercise PostgreSQL, Keycloak, OPA, OpenBao, NATS, and the selected data-plane components together.
8. Complete operational controls for metrics, alerting, audit records, incident response, data retention, disaster recovery, release signing, and rollback.
9. Reconcile legacy product material with implemented behavior and independently validated claims.

## Website Deployment Status

The website is a separate concern from the platform blockers above and is
ready to deploy, subject to the gaps recorded in
[`../operations/deployment.md`](../operations/deployment.md).

Outstanding before it carries real customer data at volume: an approved
retention and deletion schedule, secrets moved from deployment parameters to
Key Vault or managed identity, alerting on failed submissions and probe
failures, and one observed end-to-end deployment.

Production promotion is blocked until every applicable item has implementation evidence and an approved risk decision. Passing repository CI proves only the checks listed above.
