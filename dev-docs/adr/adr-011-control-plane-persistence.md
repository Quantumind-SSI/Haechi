# ADR-011: Control-plane persistence and the database driver exception

- Status: Accepted
- Date: 2026-08-30
- Supersedes: nothing
- Related: ADR-003 (endpoint language), `go.mod` dependency policy

## Context

The control plane held every tenant, device, policy, gateway and enrolment code
in memory. The composition root always selected `MemoryStore`, and the
PostgreSQL adapter, though written and tested for compilation, was never wired
to anything. `cmd/control-api` refused to start outside development precisely
because starting would have meant serving real traffic from state that does not
survive a restart.

That refusal was correct but terminal: it made the control plane undeployable,
which blocks every other piece of the platform that needs somewhere real to
authenticate against.

The module carried a stated policy of being standard-library-only. Durable
state cannot be reached from the standard library alone, so the policy and the
requirement were in direct conflict.

## Decision

**Wire PostgreSQL into the composition root, and admit one driver dependency.**

1. `github.com/jackc/pgx/v5` is added, imported for its side effect only, and
   only by `cmd/control-api`. The adapter continues to depend on `database/sql`
   alone, so the driver can be replaced without touching adapter code.

2. Durable storage becomes mandatory outside development. `HAECHI_DATABASE_URL`
   selects PostgreSQL; without it, `staging` and `production` refuse to start.
   Development still permits the in-memory store, and says so at startup.

3. Every `Store` method takes a `context.Context`, and the HTTP layer passes the
   request context through the service to the driver.

4. The connection pool is bounded, and the process pings the database at startup
   rather than discovering it is unreachable on the first request.

5. Readiness is separated from liveness. `/readyz` reports the store's
   reachability; `/healthz` reports only that the process is serving.

## Why the exception is acceptable

The stated policy exists to keep unreviewed code out of fail-closed
cryptographic paths. That reasoning does not extend to a database driver:

- It sits outside the cryptographic path entirely. Signing, the capability
  protocol, the domain model, the policy and telemetry boundaries and the agent
  core remain standard-library-only and still build offline.
- It is imported at exactly one point, the composition root, which is the layer
  whose job is to know which concrete systems are in play.
- `pgx` is the de facto PostgreSQL driver for Go, widely deployed and actively
  maintained. Writing a wire-protocol implementation instead would put far more
  unreviewed code on the path, not less.

The `go.mod` comment has been rewritten to describe this accurately rather than
continuing to claim a property the module no longer has.

## Consequences

**Gained.** The control plane can be deployed. State survives restart. A slow
database now cancels with its request instead of holding a connection nobody is
waiting for. An instance that cannot reach its database reports itself unready
rather than accepting traffic it cannot serve. Tenant isolation, transactional
enrolment and policy versioning are proven against a real database rather than
an in-memory approximation.

**Given up.** The module is no longer dependency-free. `go.sum` must be kept
current and the driver tracked for vulnerabilities, which `govulncheck` in CI
already does.

**Still open.** Row-level security is not enabled; isolation is enforced by
tenant-scoped queries and tested as such. Enrolment codes are stored as
one-way lookup keys, but no automatic expiry job removes spent ones. Backup and
restore procedures are defined for the website database only. Migrations are
applied from a single `schema.sql`, which is adequate while the schema is
additive and will need a versioned runner before the first destructive change.

## Alternatives rejected

**Build tags for the driver.** Keeps the default build dependency-free, but the
dependency remains in the module graph regardless, so the property is cosmetic.
It also produces a binary whose behaviour depends on build flags, which is a
poor trait in a security control plane.

**A different store.** No other option removes the dependency; it only changes
which one is taken.

**Continuing to refuse production.** Honest, and where this started, but it
blocks the platform indefinitely.
