# Haechi Event Contracts (NATS)

Single source of truth for asynchronous control events (Integration Document §7:
"asynchronous events use versioned schemas over NATS"). Subjects are versioned;
payloads validate against the JSON Schemas here. The Go constants live in
`internal/events` and the publisher port is wired to NATS by `adapters/nats`.

| Subject | Schema | Producer |
| --- | --- | --- |
| `haechi.device.enrolled.v1` | `device-enrolled.v1.json` | control-api |
| `haechi.device.revoked.v1` | `device-revoked.v1.json` | control-api |
| `haechi.gateway.health.v1` | `gateway-health.v1.json` | gateway-controller |
| `haechi.policy.published.v1` | `policy-published.v1.json` | control-api |

Rules:
- Events carry **no** durable user identifier, payload or DNS content (they share
  the telemetry prohibition list, dev-docs/threat-model/telemetry-prohibition-list.md).
- Breaking a schema requires a new `.vN` subject and a compatibility test.
