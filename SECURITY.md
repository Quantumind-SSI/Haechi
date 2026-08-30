# Security Policy

## Supported Code

Haechi is a pre-production engineering project. Only the latest revision of `main` receives security fixes. No current commit is approved for production network protection, anonymity, or post-quantum security claims.

## Reporting A Vulnerability

Use the repository's **Security** tab to open a private vulnerability report, or email <security@haechi.co.uk>. Do not disclose vulnerabilities in public issues, pull requests, discussions, logs, or test fixtures.

Email to that address is forwarded, not delivered to a monitored mailbox with an on-call rotation. Expect acknowledgement within five working days. Do not send exploit details in an initial unencrypted message; describe the affected component and impact, and wait for a secure channel to be arranged.

Include the affected revision, component, impact, reproduction steps, and any known mitigation. Remove credentials, personal data, private keys, capability tokens, and customer identifiers from the report.

The repository owner will acknowledge the report through GitHub, coordinate validation privately, and publish remediation information after a fix is available. A disclosure date must be agreed before public discussion.

## Security Boundaries

The control API executable is intentionally restricted to `HAECHI_ENV=development` while durable storage and production service identity are incomplete. The endpoint tunnel, shared replay store, and RFC 9474 capability implementation remain release blockers documented in `dev-docs/engineering/readiness.md`.
