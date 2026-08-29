# Architecture Decision Records

Each ADR is immutable once **Accepted**. Supersede rather than edit. ADRs are
mandatory before the corresponding implementation is treated as production
security (Integration Document §17, Appendix B).

| ADR                                          | Title                                                           | Status                                   |
| -------------------------------------------- | --------------------------------------------------------------- | ---------------------------------------- |
| [ADR-001](adr-001-naming.md)                 | Canonical product and component naming                          | Accepted                                 |
| ADR-002                                      | NetBird integration vs maintained QuantmindSSI fork             | Proposed                                 |
| [ADR-003](adr-003-endpoint-language.md)      | Endpoint language, process boundary and privileged helper model | Accepted                                 |
| [ADR-004](adr-004-capability-trust-model.md) | Enterprise identity and privacy-capability trust model          | Accepted                                 |
| [ADR-005](adr-005-pq-reinforcement.md)       | WireGuard post-quantum reinforcement and downgrade policy       | Accepted                                 |
| ADR-006                                      | Direct and protected transport interface                        | Proposed                                 |
| ADR-007                                      | Telemetry prohibition list and GIM data contract                | Accepted (see threat-model)              |
| ADR-008                                      | SaaS, dedicated and disconnected deployment boundaries          | Proposed                                 |
| ADR-009                                      | Dependency pinning, fork and upstream contribution policy       | Accepted (see third_party/manifest.yaml) |
| ADR-010                                      | Release evidence and permitted security claims                  | Proposed                                 |
