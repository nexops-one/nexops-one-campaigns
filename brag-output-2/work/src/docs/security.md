# Security and data flow

This page describes how the engine protects stored data. Access control and
the audit log are described in [access.md](access.md); evidence handling in
[workflow.md](workflow.md).

The engine makes **no outbound network calls** by default: no telemetry, no
update checks, no evidence fetching (unless you allow evidence hosts with
`COMPLIANCE_EVIDENCE_FETCH_ALLOW`).

## Data flow

| Data | Enters through | Stored | Leaves through |
|---|---|---|---|
| Register records | adapters (`POST /api/v1/ingestions`), spreadsheet import (API or console) | PostgreSQL, sensitive fields encrypted | API reads, console pages, Profile B (field names and counts only, never values) |
| Evidence references | API or console (title, location, checksum) | location encrypted; the object stays in your systems | API and console reads, audited as `evidence.access`; reports show scheme and host only |
| Evidence files (optional) | `POST /api/v1/evidence/upload`, only with `COMPLIANCE_EVIDENCE_STORAGE_DIR` | encrypted files on disk | `GET /api/v1/evidence/{id}/content`, audited |
| Workflow notes and reasons | API or console | encrypted | history reads; never in the audit log or reports |
| Reports | generated from stored data | report bodies encrypted | downloads, audited as `report.download` |
| Accounts, tokens, sessions | CLI, API, console | argon2id password hashes, SHA-256 token and session hashes | never |

Outbound connections: none by default. The engine contacts an evidence host
only to verify a checksum, and only hosts listed in
`COMPLIANCE_EVIDENCE_FETCH_ALLOW`. Files are verified only under
`COMPLIANCE_EVIDENCE_ROOT`. Otherwise the customer verifies with
`compliance-engine evidence verify`, which hashes locally and submits the result.

## Console sessions

The console ([console.md](console.md)) stores only the SHA-256 of each session
value, with the user, workspace and timestamps. The cookie is `HttpOnly`,
`Secure` and `SameSite=Strict`; forms carry a CSRF token derived from the
session; responses carry `Content-Security-Policy: default-src 'self'` and
forbid framing. Sessions expire after `COMPLIANCE_SESSION_IDLE` and
`COMPLIANCE_SESSION_MAX`.

## What is encrypted at rest

When `COMPLIANCE_ENCRYPTION_KEY_FILE` is set, these values are encrypted
before they reach the database (or the in-memory store):

| Data | Fields |
|---|---|
| Canonical records | the sensitive fields listed in `schema/v0.1.0/sensitive.json`: costs (`contractual_arrangement.annual_cost`, `ict_provider.total_annual_cost`, `financial_entity.total_assets`), contract terms (`arrangement_service_line.termination_reason`, `notice_period_entity`, `notice_period_provider`), resilience and exit details (`function.criticality_reasons`, `rto`, `rpo`, `discontinuing_impact`; `service_assessment.not_substitutable_reason`, `reintegration_possibility`, `discontinuing_impact`, `alternative_providers`) |
| Evidence | the location (`uri`), the revocation reason, and uploaded files in managed storage |
| Workflow | assessment notes, and the reasons and notes of every transition |
| Reports | the facts, validation report and rendered files of every report (metadata stays readable); see [reports.md](reports.md) |

Identifiers, names, countries, dates and codes stay readable by the database
so that queries, completeness and evaluations work; they are protected by
database access control and TLS. The audit log is not encrypted, so its hash
chain can be verified without keys; it therefore never contains free text:
workflow and revocation events record `reason_given` / `note_given`, not the text.

**Content hashes.** Without encryption, record versions carry a SHA-256 of
their content. With encryption they carry an HMAC-SHA256 under a per-tenant
key (`hmac-sha256:...`), so a low-entropy value (a notice period, a cost)
cannot be recovered by hashing guesses.

Without a key file the engine logs a warning at start and
`GET /api/v1/about` reports `"encryption_at_rest": false`.

## Keys

```text
key-encryption key (KEK, a file you hold)
  └─ wraps, per tenant and version: a data key (AES-256-GCM) and a hashing key (HMAC)
       └─ seals each value, bound to its tenant, workspace, entity and field
```

- **Create the KEK:** `compliance-engine keys generate --out /etc/compliance/kek`
  writes 32 random bytes (base64) with mode 0600. The engine refuses a key
  file readable by group or others. Its key ID (16 hex characters) is logged at start.
- **Keep the KEK outside database backups.** A backup cannot be read without
  it, and losing it makes encrypted data unrecoverable. Store it like any other
  production secret (a secrets manager, or an offline copy in a safe).
- **Tenant keys** are created on first write, wrapped by the KEK and stored in
  the database (`ce_tenant_keys`). Reading never creates keys.
- **Rotate the KEK:** create a new key file, then
  `compliance-engine keys rotate --from OLD --to NEW` re-wraps every tenant's
  keys (audited as `keys.rotate_kek`); point `COMPLIANCE_ENCRYPTION_KEY_FILE` at
  the new file and restart. The old KEK stops working at once.
- **Rotate a tenant's data key:** `compliance-engine keys rotate --data --tenant t`
  adds a new active version for new writes (`keys.rotate_data`). Values written
  earlier stay readable with their version; content hashes do not change.
- **Enable encryption on existing data:** set the key file, then run
  `compliance-engine keys seal-existing --tenant t` once per tenant. It
  encrypts stored sensitive fields, evidence locations and notes, and rehashes
  record versions and provenance. The append-only transition history is not
  rewritten: reasons recorded before encryption stay as they were.

### Docker Compose

The engine runs as the non-root user `65532` in its image. To use a key file
with Compose on Linux, mount it read-only and make it readable only by that user:

```yaml
services:
  engine:
    environment:
      COMPLIANCE_ENCRYPTION_KEY_FILE: /run/secrets/compliance_kek
    volumes:
      - /etc/compliance/kek:/run/secrets/compliance_kek:ro
```

```bash
sudo chown 65532:65532 /etc/compliance/kek && sudo chmod 600 /etc/compliance/kek
```

Bind mounts on Docker Desktop (Windows, macOS) do not preserve file modes, so
the engine refuses the key file there; use a Linux host or a secrets manager
that writes the file inside the container with mode 0600.

### Kubernetes

Kubernetes mounts Secret files owned by root. The engine therefore also
accepts a key file readable by its own group and by nobody else (mode 0440 or
0640, the group being the pod's `fsGroup`); a file others can read, or its
group can write, is always refused. The Helm chart mounts the key this way
([deploy/helm/compliance-engine](../deploy/helm/compliance-engine/README.md)).

## Managed evidence storage

By default evidence is only referenced; the engine stores its location and
checksum. When `COMPLIANCE_EVIDENCE_STORAGE_DIR` is set (a KEK is required),
`POST /api/v1/evidence/upload` stores uploaded files there as
`<tenant>/<sha256>.bin`, encrypted with the tenant's data key. Downloads
(`GET /api/v1/evidence/{id}/content`) are audited as `evidence.access`.
Back up this directory together with the database.

## Retention and tenant deletion

Retention policies, their floors and tenant deletion (crypto-shredding) are
described in [retention.md](retention.md).

## Backup and restore

See [backup-restore.md](backup-restore.md). A backup cannot be restored
without the KEK that was in use when it was taken.
