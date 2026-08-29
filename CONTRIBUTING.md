# Contributing

## Toolchain

- Go 1.26 or newer within the 1.26 release line
- Node.js 20 or newer
- npm 10 or newer
- Docker for container and OPA policy checks

Install locked JavaScript dependencies with:

```bash
make web-install
```

## Required Verification

Run the local quality gates:

```bash
make verify
make audit
```

`make verify` checks formatting, workflow syntax, static analysis, race-enabled Go tests, all builds, and website tests. `make audit` checks Go and npm dependencies against vulnerability databases and requires network access.

Run the complete CI-equivalent suite on a machine with Docker:

```bash
make ci
```

The complete suite also tests the OPA bundle and builds both container images.

## Change Discipline

- Keep pull requests focused on one coherent behavior or repository control.
- Add tests for successful behavior, boundaries, and relevant failure paths.
- Preserve deny-by-default behavior at authentication, policy, capability, tunnel, and telemetry boundaries.
- Update OpenAPI, event schemas, ADRs, and operational documentation in the same change as their implementation.
- Do not commit credentials, local runtime data, generated build output, customer data, or personal data.
- Record a new ADR in `dev-docs/adr/` when a change alters a trust boundary, cryptographic construction, persistence model, or service ownership boundary, and register it in the ADR table.

## Documentation

All standalone documentation lives in `dev-docs/` and is indexed by [`dev-docs/README.md`](dev-docs/README.md). Contracts and component READMEs stay beside the code they describe and are linked from that index.

Update the index in the same change that adds, moves, or removes a document. Do not reintroduce documents at the repository root, and do not use spaces in document file names.

Before quoting any document externally, check its authority level in the index. Historical product material contains claims the codebase does not implement; `dev-docs/engineering/readiness.md` is the controlling statement of current capability.

All changes require passing CI and approval from the configured code owner. Security-sensitive changes require explicit review of threat, rollback, and operational failure modes.
