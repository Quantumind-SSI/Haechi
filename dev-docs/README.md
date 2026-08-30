# Haechi Development Documentation

This directory is the single entry point for Haechi documentation. Every standalone document in the repository is stored here and indexed below. Documentation that must sit beside the code it describes stays in place and is linked from this index.

Repository: <https://github.com/QuantmindSSI/Haechi>
Maintainer: Quantumind Ltd
Index updated: 2026-08-28

## How To Read This Set

Documents carry one of three authority levels. Check the level before quoting a document in a proposal, contract, or customer conversation.

| Level             | Meaning                                                                                                                                  |
| ----------------- | ---------------------------------------------------------------------------------------------------------------------------------------- |
| Authoritative     | Describes implemented behavior or a binding engineering decision. Safe to rely on.                                                       |
| Engineering input | Describes intended design. Not evidence that the behavior exists.                                                                        |
| Historical        | Product and investment material written ahead of implementation. Contains claims the codebase does not yet support. Not a release claim. |

The controlling statement of what actually works is [`engineering/readiness.md`](engineering/readiness.md). Where any other document conflicts with it, the readiness record wins.

## Engineering Documentation

| Document                                                                                   | Authority     | Purpose                                                                         |
| ------------------------------------------------------------------------------------------ | ------------- | ------------------------------------------------------------------------------- |
| [`engineering/readiness.md`](engineering/readiness.md)                                     | Authoritative | Component status, enforced repository gates, and production release blockers    |
| [`architecture/overview.md`](architecture/overview.md)                                     | Authoritative | Ports and adapters map, trust zones, upstream integration table                 |
| [`threat-model/telemetry-prohibition-list.md`](threat-model/telemetry-prohibition-list.md) | Authoritative | Fields that must never cross the telemetry boundary, enforced by `services/gim` |
| [`adr/README.md`](adr/README.md)                                                           | Authoritative | Architecture decision register                                                  |
| [`operations/deployment.md`](operations/deployment.md)                                     | Authoritative | Website deployment runbook for Cloudflare Pages and Azure Container Apps        |

### Architecture Decision Records

ADRs are immutable once accepted. Supersede rather than edit.

| ADR                                              | Title                                                           | Status                                               |
| ------------------------------------------------ | --------------------------------------------------------------- | ---------------------------------------------------- |
| [ADR-001](adr/adr-001-naming.md)                 | Canonical product and component naming                          | Accepted                                             |
| ADR-002                                          | NetBird integration versus maintained fork                      | Proposed, not written                                |
| [ADR-003](adr/adr-003-endpoint-language.md)      | Endpoint language, process boundary and privileged helper model | Accepted                                             |
| [ADR-004](adr/adr-004-capability-trust-model.md) | Enterprise identity and privacy-capability trust model          | Accepted                                             |
| [ADR-005](adr/adr-005-pq-reinforcement.md)       | WireGuard post-quantum reinforcement and downgrade policy       | Accepted                                             |
| ADR-006                                          | Direct and protected transport interface                        | Proposed, not written                                |
| ADR-007                                          | Telemetry prohibition list and GIM data contract                | Accepted, recorded in `threat-model/`                |
| ADR-008                                          | SaaS, dedicated and disconnected deployment boundaries          | Proposed, not written                                |
| ADR-009                                          | Dependency pinning, fork and upstream contribution policy       | Accepted, recorded in `../third_party/manifest.yaml` |
| ADR-010                                          | Release evidence and permitted security claims                  | Proposed, not written                                |

Five of the ten registered ADRs have no document. ADR-004 and ADR-010 are the two whose absence carries the most risk, because they govern cryptographic design and the claims the company is permitted to make.

## Integration Documentation

| Document                                                                                       | Authority         | Purpose                                                                                                            |
| ---------------------------------------------------------------------------------------------- | ----------------- | ------------------------------------------------------------------------------------------------------------------ |
| [`integration/haechi-integration-document.html`](integration/haechi-integration-document.html) | Engineering input | Integration and delivery blueprint, browser readable. Source of the section numbers cited throughout the Go source |
| [`integration/haechi-integration-document.docx`](integration/haechi-integration-document.docx) | Engineering input | Same content in editable form                                                                                      |

Go source comments cite this document by section, for example `§6.2` in `services/control-api`. It describes the target design. It is not evidence of implementation.

## Product And Planning Documentation

| Document                                                                                   | Authority         | Purpose                                                   |
| ------------------------------------------------------------------------------------------ | ----------------- | --------------------------------------------------------- |
| [`product/haechi-executive-summary.pdf`](product/haechi-executive-summary.pdf)             | Historical        | Investment and positioning summary                        |
| [`product/haechi-vpn-software.pdf`](product/haechi-vpn-software.pdf)                       | Historical        | Early product specification                               |
| [`product/haechi-research-plan.docx`](product/haechi-research-plan.docx)                   | Historical        | Research and delivery plan                                |
| [`product/haechi-website-design-document.pdf`](product/haechi-website-design-document.pdf) | Engineering input | Website design specification, implemented in `../website` |

### Claim Risk Notice

The executive summary and the early product specification describe capabilities that the codebase does not implement, including provable anonymity, a cryptographic no-logs guarantee, zero-knowledge proofs, traffic morphing, and quantum resistance. `engineering/readiness.md` records the implementation status of each area.

These documents are retained for historical and internal context. They must not be issued to customers, investors, or partners as descriptions of current capability until the corresponding release gates pass and ADR-010 defines permitted claims.

## Brand Documentation

| Document                                                                                                                                 | Authority                     | Purpose                                                                                                                |
| ---------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------- | ---------------------------------------------------------------------------------------------------------------------- |
| [`brand/haechi-brand-designer-guidelines.pdf`](brand/haechi-brand-designer-guidelines.pdf)                                               | Authoritative for design work | Brand identity guidelines                                                                                              |
| [`brand/haechi-product-branding-and-creative-direction.pdf`](brand/haechi-product-branding-and-creative-direction.pdf)                   | Authoritative for design work | Product branding and creative direction                                                                                |
| [`brand/haechi-product-branding-and-creative-direction-document.pdf`](brand/haechi-product-branding-and-creative-direction-document.pdf) | Superseded candidate          | Shorter, earlier variant of the creative direction document. Retained pending confirmation of which version is current |

Marketing and design owners should confirm which creative direction document is current and mark the other as superseded.

## Documentation Held Beside Its Code

The following documents stay next to the artifacts they describe, because separating them would let them drift. They form part of this documentation set.

| Document                                                                     | Purpose                                                           |
| ---------------------------------------------------------------------------- | ----------------------------------------------------------------- |
| [`../README.md`](../README.md)                                               | Repository entry point, engineering status, verification commands |
| [`../CONTRIBUTING.md`](../CONTRIBUTING.md)                                   | Toolchain, required verification, change discipline               |
| [`../SECURITY.md`](../SECURITY.md)                                           | Vulnerability reporting process and security boundaries           |
| [`../api/openapi/control-api.yaml`](../api/openapi/control-api.yaml)         | Administrative HTTP contract                                      |
| [`../api/events/README.md`](../api/events/README.md)                         | Versioned NATS event contracts and their schemas                  |
| [`../api/proto/haechi/v1/haechi.proto`](../api/proto/haechi/v1/haechi.proto) | Service definitions, not yet generated or served                  |
| [`../third_party/manifest.yaml`](../third_party/manifest.yaml)               | Pinned upstream dependencies, review status, and release gates    |
| [`../website/README.md`](../website/README.md)                               | Website runtime and configuration                                 |

## Where New Documentation Goes

| Kind of document                                                                                             | Location                                                    |
| ------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------- |
| Decision that changes a trust boundary, cryptographic construction, persistence model, or ownership boundary | New ADR in `adr/`, registered in the table above            |
| Change to implementation status or release blockers                                                          | `engineering/readiness.md`                                  |
| Change to what telemetry may cross the privacy boundary                                                      | `threat-model/telemetry-prohibition-list.md`                |
| Component, port, or adapter change                                                                           | `architecture/overview.md`                                  |
| Runbook, deployment, or incident procedure                                                                   | `operations/`                                               |
| API or event contract                                                                                        | Alongside the contract in `../api/`, linked from this index |

Update this index in the same change that adds or removes a document.

## Conventions

- File names are lower case, hyphen separated, with no spaces. Names with spaces break URLs, shell commands, and some CI tooling.
- Documents that supersede others must say so in their own text, not only in this index.
- Binary formats are stored as delivered. The repository marks `.pdf` and `.docx` as binary in `.gitattributes` so Git does not attempt line based merges.

## Reorganisation Record, 2026-08-28

All standalone documents were moved from the repository root and from the former `docs/` directory into this directory. Renames are recorded so earlier references can be resolved.

| Former path                                                   | Current path                                                        |
| ------------------------------------------------------------- | ------------------------------------------------------------------- |
| `Haechi Integration Document.docx`                            | `integration/haechi-integration-document.docx`                      |
| `Haechi Integration Document.html`                            | `integration/haechi-integration-document.html`                      |
| `Haechi Executive Summary.pdf`                                | `product/haechi-executive-summary.pdf`                              |
| `Haechi-VPN SOFTWARE .pdf`                                    | `product/haechi-vpn-software.pdf`                                   |
| `Haechi Website Design Document.pdf`                          | `product/haechi-website-design-document.pdf`                        |
| `Haechi-research plan..docx`                                  | `product/haechi-research-plan.docx`                                 |
| `Haechi Brand Designer Guidelines.pdf`                        | `brand/haechi-brand-designer-guidelines.pdf`                        |
| `Haechi Product Branding and Creative Direction.pdf`          | `brand/haechi-product-branding-and-creative-direction.pdf`          |
| `Haechi Product Branding and Creative Direction Document.pdf` | `brand/haechi-product-branding-and-creative-direction-document.pdf` |
| `docs/adr/`                                                   | `adr/`                                                              |
| `docs/architecture/`                                          | `architecture/`                                                     |
| `docs/engineering/`                                           | `engineering/`                                                      |
| `docs/threat-model/`                                          | `threat-model/`                                                     |

Three files were byte-identical copies of documents already listed above and were removed. Each was confirmed identical by SHA-256 before removal, so no content was lost.

| Removed duplicate                                      | SHA-256       | Identical to                                               |
| ------------------------------------------------------ | ------------- | ---------------------------------------------------------- |
| `Haechi Brand Designer Guidelines 2.pdf`               | `c4cc7517...` | `brand/haechi-brand-designer-guidelines.pdf`               |
| `Haechi Product Branding and Creative Direction 2.pdf` | `9ca180d2...` | `brand/haechi-product-branding-and-creative-direction.pdf` |
| `Haechi plan..docx`                                    | `2e8e5c7f...` | `product/haechi-research-plan.docx`                        |
