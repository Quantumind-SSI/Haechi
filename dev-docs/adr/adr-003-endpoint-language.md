# ADR-003: Endpoint language, process boundary and privileged helper model

- Status: Accepted
- Date: 2026-08-27
- Deciders: Agent team, Security team

## Context

The endpoint agent (Integration Document §6.1) is the only component allowed to
coordinate tunnel state on a device. It must run on Windows, macOS, Linux,
Android and iOS, interoperate with the WireGuard and Rosenpass ecosystems, and
keep a small, auditable trusted computing base on the fail-closed path.

The wider building-block ecosystem we integrate with, including NetBird,
wireguard-go, OPA, CIRCL, NATS, and OpenBao, is predominantly Go.

## Decision

1. **The agent core, control plane services and capability protocol are written
   in Go**, matching the upstream ecosystem and giving us memory-safe,
   statically-linked, cross-compiled binaries.

2. **The security-critical core depends only on the Go standard library.** The
   capability protocol, policy/route signature verification and kill-switch
   logic MUST NOT import unreviewed third-party cryptography. Standardised
   primitives are taken from `crypto/*` (Ed25519, RSA per RFC 9474, SHA-256,
   HMAC). This keeps the TCB small and builds reproducible/offline.

3. **Privilege is split.** A minimal privileged helper performs only the
   operations that require elevation (interface/route/DNS/firewall changes)
   behind a narrow, authenticated local IPC. The unprivileged agent core holds
   no ambient OS privilege and drives the helper through an explicit contract.

4. **Platform tunnels are adapters, never forks.** wireguard-go (desktop),
   WireGuardKit / Network Extension (Apple) and the Android `VpnService` sit
   behind the `agent/tunnel.Tunnel` interface. Replacing an adapter never
   changes core state-machine behaviour.

## Consequences

- One language and one crypto surface across control plane and agent lowers
  review cost and eliminates a class of FFI/memory bugs.
- Mobile still needs thin native shells (Swift/Kotlin) over a shared Go core
  via gomobile; those shells contain no security decisions.
- Choosing stdlib-only for the core means some advanced primitives (e.g. richer
  anonymous-credential schemes) live behind a reviewed dependency (CIRCL) and a
  release gate rather than in the default build. See ADR-004.
