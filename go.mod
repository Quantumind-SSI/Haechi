module github.com/QuantmindSSI/haechi

go 1.26

// Dependency policy.
//
// The security-critical core remains standard-library-only: signing, the
// capability protocol, the domain model, the policy and telemetry boundaries,
// and the agent core. Nothing on a fail-closed path pulls in unreviewed
// cryptographic code, and those packages still build offline.
//
// The single exception is the PostgreSQL driver below. Durable control-plane
// state cannot be reached from the standard library alone, and an in-memory
// store loses every tenant, device and policy on restart. The driver is
// imported only by cmd/control-api, the composition root; adapters/postgres
// depends on database/sql alone, so the driver can be replaced without
// touching adapter code. It sits outside the cryptographic path entirely.
// See dev-docs/adr/adr-011-control-plane-persistence.md.
//
// Upstream building blocks (NetBird, wireguard-go, Rosenpass, OPA, Keycloak,
// OpenBao, NATS, ClickHouse, ...) are consumed as external services or pinned
// binaries per third_party/manifest.yaml, NOT vendored into this module.

require github.com/jackc/pgx/v5 v5.11.0

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	golang.org/x/sync v0.21.0 // indirect
	golang.org/x/text v0.39.0 // indirect
)
