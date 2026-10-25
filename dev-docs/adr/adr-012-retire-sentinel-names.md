# ADR-012: Retirement of the Sentinel product names

- Status: Accepted
- Date: 2026-08-30
- Deciders: Product, Engineering
- Supersedes: the third bullet of the Decision in [ADR-001](adr-001-naming.md)

## Context

[ADR-001](adr-001-naming.md) fixed the product name as **Haechi** but did not
close the question of the earlier names. Its third decision bullet reads:

> "Quantumind Sentinel" and "Q-Sentinel" are retained only if formally approved
> as architecture or edition names; they are not used in code identifiers.

That approval was never granted, and the conditional was never resolved. Three
consequences followed.

Two historical documents still lead with the retired names: `haechi-vpn-software.pdf`
is titled "Quantumind Sentinel: Technical Implementation and Integration Plan"
and uses them 18 times, and `haechi-research-plan.docx` is headed "QUANTUMIND
SENTINEL" and uses them 25 times. Both are the fullest product and commercial
descriptions the company has, so they are the documents most likely to be
reached for when someone needs to explain Haechi.

The integration document, which is live engineering input and is cited by
section from 33 places in the Go source, carried the open question forward in
Appendix A rather than resolving it.

An unresolved naming option is not a neutral state. It licenses inconsistency in
exactly the material that goes to customers, investors and partners, and it
compounds the claim risk already recorded against those documents.

ADR-001's own Consequences require a superseding ADR for a naming change, so
this decision is recorded here rather than by editing ADR-001, which remains
immutable.

## Decision

1. **"Quantumind Sentinel" and "Q-Sentinel" are retired.** They are not
   approved as architecture names, edition names, or in any other role. The
   conditional in ADR-001's third bullet is closed negatively.

2. **Haechi is the only product name** used in code, documentation, user
   interface copy, and external material. This restates and does not alter
   ADR-001's first bullet, which continues to govern the product and
   organisation names.

3. **Component names in the retired architecture are also retired.** The
   Sentinel-era layer names COL, DPCO, QSVG and ZKO have no current meaning.
   The five layers are the endpoint agent, privacy-separated access, the
   control plane, the regional data plane, and the Global Intelligence Mesh, as
   recorded in [`../architecture/overview.md`](../architecture/overview.md).
   ZKO in particular must not be reintroduced: ADR-004 prohibits any
   zero-knowledge claim before ADR-010 and external cryptographic review.

4. **Live documents are corrected. Historical documents are not rewritten.**
   A historical record that is edited stops being evidence of what was believed
   and when, and the documentation conventions store binary formats as
   delivered. Correcting only the names in those files would also leave their
   prohibited capability claims intact while making them look current, which is
   worse than leaving them visibly dated. They are instead covered by the
   Naming Correction Notice in [`../README.md`](../README.md).

## Consequences

- Appendix A of the integration document was amended on 2026-08-30 to record
  this decision instead of restating the open question. Both the `.html` and
  `.docx` renderings were updated together.
- The index gains a Naming Correction Notice mapping the retired names in the
  two historical documents onto Haechi.
- `pkg/capability/capability.go` uses the word "sentinel" in its established
  Go sense, a package-level error value compared with `errors.Is`. That usage
  is unrelated to the product name and is unaffected.
- Reintroducing either retired name requires a superseding ADR.
