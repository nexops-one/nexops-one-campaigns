# Security policy

## Reporting a vulnerability

Report vulnerabilities privately to **security@compliance-engine.invalid** (placeholder: the disclosure address will be published before the first public release). Do not open a public issue or pull request.

Include the affected version (`compliance-engine version`), the configuration involved (without secrets), the steps to reproduce, and the impact you observed.

We aim to acknowledge a report within 3 working days and to agree on a disclosure date with you. Fixes are released for the latest minor version.

## Supported versions

| Version | Supported |
|---|---|
| 0.1.x | yes |

## Scope

compliance-engine is self-hosted. It makes no outbound calls by default and sends no telemetry. The security design (encryption at rest, keys, evidence handling, sessions, audit log) is described in [docs/security.md](docs/security.md).

Release artifacts come with SHA-256 checksums, an SBOM and, when signed, `cosign` signatures. How to verify them: [docs/install.md](docs/install.md).
