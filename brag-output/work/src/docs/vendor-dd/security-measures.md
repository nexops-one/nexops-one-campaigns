Status: pending legal review

# Security measures

A summary for due diligence. The details, with every setting, are in the
product documentation linked from each line.

## Access control

- Roles per workspace (`auditor`, `owner`, `reviewer`, `approver`, `admin`), additive, checked on every API route and console page against one permission matrix ([../access.md](../access.md), [../reference/roles.md](../reference/roles.md)).
- Separation of duties: a control's owner cannot approve it unless the deployment explicitly allows it, and then every such approval is flagged; the enterprise edition can require several approvers and a reviewer's recommendation.
- Local accounts with argon2id password hashes; console sessions with `HttpOnly`, `Secure`, `SameSite=Strict` cookies, CSRF tokens, idle and absolute timeouts; failed sign-ins rate-limited per address.
- API tokens scoped to one workspace, with roles no larger than their creator's, optional expiry, shown once and stored as SHA-256 hashes, revocable at once.
- SSO with OpenID Connect (authorization code with PKCE, ID token verification) in the enterprise edition.
- Rate and size limits per token ([../server.md](../server.md)).

## Audit

- One append-only, hash-chained audit event per privileged action: sign-ins, member and token changes, ingestions and imports, workflow actions, evidence access and checks, report downloads, retention runs, key rotation, license installation ([../audit.md](../audit.md)).
- PostgreSQL refuses updates and deletes of audit events; `compliance-engine audit verify` recomputes the chain and reports the first broken link.
- The audit log holds no free text and no secret value.

## Data protection

- Encryption at rest of sensitive register fields, evidence locations and files, workflow texts and report bodies, under a per-tenant data key wrapped by a key-encryption key the customer holds; AES-256-GCM; key rotation; content hashes keyed per tenant ([../security.md](../security.md)).
- Retention policies with floors, and tenant deletion by destroying the tenant's keys (crypto-shredding) ([../retention.md](../retention.md)).
- Evidence stays in the customer's systems by default: the engine keeps its location and checksum, and integrity checks run locally.
- Reports redact evidence locations and never contain the sensitive fields (Profile A, the regulatory export, contains what the authority requires).

## Network

- No outbound connection by default: no telemetry, no update check, offline license and catalog-bundle verification.
- TLS by the engine or a reverse proxy; the Helm chart can restrict ingress and egress with a NetworkPolicy.

## Software supply chain and build

- Builds from a public repository (open core) with a pinned Go toolchain and `-trimpath`; distroless, non-root container images, run with a read-only root filesystem by the Helm chart.
- SHA-256 checksums, SPDX SBOMs and cosign signatures for releases ([sbom-and-signatures.md](sbom-and-signatures.md)).
- Contributions are signed off (Developer Certificate of Origin, checked in CI); dependencies are few and permissively licensed.

## Vendor organization

TO BE COMPLETED: the vendor's own security organization, certifications (if
any), personnel screening, vulnerability management service levels and secure
development policy. The vulnerability disclosure contact is in
[../../SECURITY.md](../../SECURITY.md).
