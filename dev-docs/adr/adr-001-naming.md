# ADR-001: Canonical product and component naming

- Status: Accepted
- Date: 2026-08-27
- Deciders: Product, Engineering

## Decision

- The product is **Haechi**, a collaborative product of **Quantumind SSI** and
  **Haechi Internet Security Services**. Development is carried out under
  Quantumind SSI.
- The canonical source organisation is **QuantmindSSI**; the Go module path is
  `github.com/QuantmindSSI/haechi`.
- "Quantumind Sentinel" and "Q-Sentinel" are retained only if formally approved
  as architecture or edition names; they are not used in code identifiers.
- Component names are fixed: `agent`, `control-api`, `entitlement`,
  `capability-issuer`, `capability-verifier`, `route-orchestrator`,
  `gateway-controller`, `gim`.

## Consequences

Package paths, service names, telemetry namespaces and Helm releases all derive
from these names. Renames after Phase 1 require a superseding ADR.
