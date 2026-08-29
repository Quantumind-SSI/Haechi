# Telemetry Prohibition List (GIM Data Contract)

Source of truth for the privacy boundary enforced by `services/gim`
(Integration Document §6.6, §12.3, ADR-007). The boundary **drops** any event
containing a prohibited field and records only that a drop occurred (never the
prohibited value).

## Prohibited: MUST NEVER cross the telemetry boundary

| Field family             | Examples                                   | Rationale            |
| ------------------------ | ------------------------------------------ | -------------------- |
| Traffic payload          | packet bytes, request/response bodies      | §12.3 no raw payload |
| DNS query content        | full domain names, resolved answers        | reveals browsing     |
| Durable user identifier  | user id, email, IdP subject, account id    | breaks unlinkability |
| Raw source address       | client public IP, MAC                      | re-identification    |
| Capability token / nonce | blinded/unblinded token, token nonce       | links sessions       |
| Secrets / keys           | private keys, PSKs, bearer tokens, cookies | secret exposure      |
| Precise identity token   | Keycloak/OIDC/SAML assertions              | zone violation       |

## Allowed: coarse, aggregate operational signals only

| Field            | Constraint                                 |
| ---------------- | ------------------------------------------ |
| `gateway_id`     | infrastructure identity, not a user        |
| `region`         | coarse geography                           |
| `service_class`  | low-cardinality class, never a destination |
| `pq_mode`        | `hybrid-pq` \| `classical`                 |
| `outcome`        | `success` \| `failure` \| `denied`         |
| `latency_bucket` | bucketed, not raw timing                   |
| `epoch`          | signing/policy epoch                       |
| counters         | pre-aggregated on the endpoint             |

## Enforcement

- Endpoint aggregates and redacts _before_ emission.
- The boundary re-validates against an **allowlist** (default-deny keys). An
  unknown key is dropped, not passed through.
- CI includes an automated proof that prohibited fields are dropped
  (`services/gim` tests), which is a required release gate (§12.3).
