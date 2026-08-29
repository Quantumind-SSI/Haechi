# ADR-004: Enterprise identity and privacy-capability trust model

- Status: Accepted
- Date: 2026-08-27
- Deciders: Crypto team, Security team, Control-plane team

## Context

Haechi must separate _enterprise identity_ from _gateway admission_ so that a
gateway can admit an entitled subject **without learning who they are** and
without the identity plane learning which gateway session followed
(Integration Document §4.1, §6.3). This is the product's core differentiator and
its highest-risk claim.

Trust zones (§4.1) require:

- Identity plane knows the user; must not know the gateway session.
- Capability issuer knows _that_ an entitled subject requested a bounded
  capability; must not know the future gateway session.
- Gateway verifies a valid capability locally; must not learn durable identity.

## Decision

The Release-1 capability is a **blindly-signed, audience-bound, epoch-limited
bearer token** built on **RSA blind signatures (RFC 9474, RSABSSA)**:

1. **Issuance is blinded.** The endpoint locally generates a random token,
   blinds it, and the issuer signs the blinded message after the entitlement
   service confirms entitlement. The issuer never sees the unblinded token, so
   it cannot link issuance to later presentation. _Unlinkability is structural,
   not policy._
2. **Coarse scoping prevents fingerprinting.** A capability binds only an
   `audience` (a coarse gateway _class_, never a browsing destination), a
   `service_class`, and an `epoch`. Scopes are deliberately low-cardinality.
3. **Epoch-limited.** Capabilities are valid only within a short signing epoch;
   the issuer rotates keys per epoch and publishes only the current/previous
   public keys. Expired epoch => deny.
4. **Replay-resistant.** The verifier enforces single-use per epoch via a
   bounded double-spend cache keyed by the token's unlinkable nonce. The nonce
   reveals nothing about identity.
5. **Fail closed.** Unknown key id, wrong audience, expired epoch, malformed
   token, or a replay all deny admission. There is no degraded "allow" path.
6. **The identity-plane session ends before gateway connection** so the two
   cannot be correlated by liveness.

## Alternatives considered

- **Anonymous credentials / blind BBS / Privacy Pass with CIRCL.** Richer
  (multi-show, attribute proofs) but larger unreviewed crypto surface. Deferred
  behind ADR-003 stdlib-only rule and an external-audit gate; RFC 9474 gives us
  a standardised, auditable Release-1 baseline now.
- **Bulletproofs / bespoke ZK.** Rejected for Release 1: "must not be adopted
  simply because a repository exists" (§6.3 security gate).

## Consequences

- Real, structural unlinkability against an _honest-but-curious_ issuer using
  only standard primitives.
- Requirements NOT met by this scheme and therefore gated as **research, not a
  security claim** until independent review (§6.3 warning):
  - issuer↔verifier _collusion_ resistance beyond timing hygiene,
  - efficient revocation before epoch expiry,
  - rate-limiting an anonymous bearer token without re-introducing linkage.
- The scheme is implemented in `pkg/capability` with published test vectors and
  negative tests (wrong audience, expired epoch, tampered signature, replay).
- No production "anonymous" or "zero-knowledge" marketing claim until ADR-010
  evidence and an external cryptographic review exist.
