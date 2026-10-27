# Installation

compliance-engine is one binary (or one distroless container image) and a
PostgreSQL database. It makes no outbound calls and sends no telemetry.

To try it first, run the demo instead: [demo.md](demo.md).

## 1. Get the release

A release holds, per version:

| File | Content |
|---|---|
| `compliance-engine_<version>_<os>_<arch>[.exe]` | the binary for linux, darwin and windows, amd64 and arm64 |
| `compliance-engine_<version>_image_linux_<arch>.docker.tar` | the container image for one architecture, for `docker load` |
| `compliance-engine_<version>_image.oci.tar` | the multi-arch container image (linux/amd64, linux/arm64) as an OCI archive, for registries |
| `SHA256SUMS` | SHA-256 of every file above |
| `sbom-source.spdx.json`, `sbom-image-linux-<arch>.spdx.json` | software bills of materials (SPDX) of the source tree and of each image |
| `SHA256SUMS.sig`, `*.spdx.json.sig`, `cosign.pub` | `cosign` signatures and the public key, when the release is signed |
| `compliance-engine_<version>_offline.tar.gz` | everything above plus Compose files, catalogs and documentation, for air-gapped sites ([deploy/offline/README.md](../deploy/offline/README.md)) |

Verify before you install:

```bash
sha256sum -c SHA256SUMS --ignore-missing
# signed releases (signed offline: there is no transparency log entry to check)
cosign verify-blob --insecure-ignore-tlog=true --key cosign.pub --signature SHA256SUMS.sig SHA256SUMS
```

Load the image with `docker load -i compliance-engine_<version>_image_linux_amd64.docker.tar`
(or `arm64`), or copy the OCI archive into your registry
(`skopeo copy oci-archive:compliance-engine_<version>_image.oci.tar docker://registry.example/compliance-engine:<version>`).

## 2. Prepare PostgreSQL

PostgreSQL 16 (the version the tests and Compose files use). Create a database and a role; in production, run the
engine with a role that does not own the tables, so that the audit log
triggers cannot be dropped by the application ([audit.md](audit.md)).

```sql
CREATE DATABASE compliance;
CREATE ROLE compliance_app LOGIN PASSWORD '...';
```

Apply the schema once as the owner with `compliance-engine migrate up`, or let
the server do it on start (`COMPLIANCE_AUTO_MIGRATE=true`, the default).

## 3. Create the key-encryption key

Encryption at rest is optional in the open core, and recommended:

```bash
compliance-engine keys generate --out /secrets/compliance-kek   # 32 random bytes, base64, mode 0600
```

Set `COMPLIANCE_ENCRYPTION_KEY_FILE=/secrets/compliance-kek`. Keep the KEK in
your secret store and **separately from database backups**: a backup cannot
be restored without it ([backup-restore.md](backup-restore.md)). Details:
[security.md](security.md).

## 4. Create the first admin user

Users are local accounts with generated passwords (shown once):

```bash
export COMPLIANCE_DATABASE_URL=postgres://compliance_app:...@db:5432/compliance
compliance-engine user create --tenant acme --email admin@acme.example --workspace register-2026 --role admin
compliance-engine user create --tenant acme --email owner@acme.example --workspace register-2026 --role owner
```

The admin then adds members and roles from the console's Settings page
([console.md](console.md)). Roles: [reference/roles.md](reference/roles.md).

For API clients, create a stored token (`compliance-engine token create`) or,
for a first bootstrap, a static token in `COMPLIANCE_TOKENS`
([server.md](server.md)). The server refuses to start when it has neither a
static token nor an active stored token.

## 5. Run

```bash
COMPLIANCE_DATABASE_URL=... \
COMPLIANCE_ENCRYPTION_KEY_FILE=/secrets/compliance-kek \
COMPLIANCE_CONSOLE_TENANT=acme \
compliance-engine serve
```

Or with Docker Compose: [deploy/docker-compose.yml](../deploy/docker-compose.yml)
and [server.md](server.md). The console is at `/console/`. Its session cookie
is `Secure`: terminate TLS in front of the engine (or set
`COMPLIANCE_TLS_CERT_FILE` and `COMPLIANCE_TLS_KEY_FILE`); only `localhost`
works over plain HTTP.

Every variable: [reference/configuration.md](reference/configuration.md).

**On Kubernetes:** the Helm chart [deploy/helm/compliance-engine](../deploy/helm/compliance-engine)
runs the engine non-root with a read-only root filesystem, migrates the
schema with a hook Job, and references Secrets for the database URL, the
key-encryption key, bootstrap tokens and the license (it never holds a secret
value). Its [README](../deploy/helm/compliance-engine/README.md) gives the
commands; `scripts/check-helm.sh` lints and validates it.

## 6. Check

```bash
curl -fsS https://compliance.acme.example/healthz
curl -fsS -H "Authorization: Bearer $TOKEN" https://compliance.acme.example/api/v1/about   # encryption_at_rest: true
compliance-engine audit verify --tenant acme
```
