module github.com/QuantmindSSI/haechi

go 1.26

// Haechi core is deliberately standard-library-only for the security-critical
// control plane, capability protocol and agent core. This keeps the trusted
// computing base small, builds reproducible and offline, and avoids pulling
// unreviewed cryptographic dependencies into fail-closed paths.
//
// Upstream building blocks (NetBird, wireguard-go, Rosenpass, OPA, Keycloak,
// OpenBao, NATS, ClickHouse, ...) are consumed as external services or pinned
// binaries per third_party/manifest.yaml, NOT vendored into this module.
