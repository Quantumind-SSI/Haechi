# ADR-005: WireGuard post-quantum reinforcement and downgrade policy

- Status: Accepted
- Date: 2026-08-27
- Deciders: Crypto team, Agent team, Gateway team

## Context

Harvest-now-decrypt-later means quantum-resistant protection must be in place
_before_ an attack is observable; it cannot be switched on after detecting a
"quantum signature" (Integration Document §2.2). WireGuard already supports a
symmetric pre-shared key (PSK) mixed into its handshake.

## Decision

1. **Hybrid, never replacement.** Rosenpass performs a post-quantum key
   exchange whose output _refreshes the WireGuard PSK_. The classical X25519
   handshake is retained; security is the max of both, never less than
   stock WireGuard.
2. **No silent downgrade.** If PQ reinforcement is _required_ by policy for a
   route and Rosenpass negotiation fails, the agent **fails closed** (no
   tunnel). If PQ is _preferred_, downgrade to classical-only is permitted only
   when policy explicitly allows it and the event is recorded as a first-class,
   operator-visible signal (never silent).
3. **Downgrade state is observable.** The negotiated mode (`hybrid-pq` vs
   `classical`) is exposed to operators via minimised telemetry without
   exposing keys or user activity.
4. **Isolation.** Rosenpass runs as a companion process controlled by the agent
   and gateway, behind the tunnel interface, so a fault in PQ code cannot
   corrupt authenticated WireGuard ciphertext.

## Consequences

- The `agent/tunnel` interface carries a `PQMode` requirement and reports the
  negotiated mode; `route-orchestrator` route plans declare whether PQ is
  `required` or `preferred`.
- Interop and downgrade-prevention tests are release gates before any
  post-quantum claim (§12.3).
