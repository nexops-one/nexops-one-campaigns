Status: pending legal review

# Exit plan

DORA (Article 28(8)) asks financial entities to be able to exit an ICT
service without disruption. This page says how to take everything out of the
compliance engine and stop using it, at any time, without the vendor.

## No lock-in by construction

- **The data model is public.** The canonical schema (`schema/v0.1.0/schema.json`, JSON Schema with register references) and its field reference are published with the open core, under Apache-2.0.
- **The software is open core.** The open-core edition is Apache-2.0: a customer can keep running, modifying or having someone else maintain the version it has, without the vendor.
- **The data stays with the customer.** In every mode offered today the database runs in the customer's environment ([subprocessors.md](subprocessors.md)).
- **A lapsed license hides nothing.** When an enterprise license expires: commercial features keep working during the grace period with a warning, then are refused; stored data, evaluations, reports and the audit log stay readable, downloadable and verifiable; the open-core features keep working; nothing is deleted ([../editions.md](../editions.md)).

## What to export, and how

| Data | Export | Format |
|---|---|---|
| Register records | `GET /api/v1/snapshots/current/records/{entity}` for each entity | JSON, canonical field names (the records of a canonical batch), with per-field states |
| Register as spreadsheets | re-export through the import templates (`GET /api/v1/templates.xlsx`) filled from the records above | CSV, XLSX |
| Register of information for the authority | Profile A report package (enterprise; indicative) | xBRL-CSV |
| Evaluations and status | `GET /api/v1/evaluations/{id}`, `GET /api/v1/status` | JSON |
| Reports | `GET /api/v1/reports/{id}/files/{name}` | JSON, PDF, xBRL-CSV ZIP, byte-for-byte as generated |
| Evidence references | `GET /api/v1/evidence` (objects stay in your systems); managed files with `GET /api/v1/evidence/{id}/content` | JSON, original files |
| Workflow history | `GET /api/v1/assessments/{catalog}/{control}` | JSON |
| Audit log | `GET /api/v1/audit?verify=true` (paged) | JSON, with the hash chain to verify it after export |
| Everything | `pg_dump` of the database, with the key-encryption key ([../backup-restore.md](../backup-restore.md)) | PostgreSQL dump |

Exports are reads: they need no license and are themselves audited where the
data is sensitive (evidence, reports).

## Steps

1. Export the data above (or keep a database dump and the key-encryption key: the open core can read it later).
2. Verify the exported audit log's chain (`?verify=true`) and keep it for the retention period your obligations require.
3. Stop the adapters and remove their tokens; stop the engine.
4. Delete the data: `compliance-engine tenant delete --tenant <id> --yes` deletes the tenant's data and destroys its keys, which also makes the copies in old backups unreadable ([../retention.md](../retention.md)). Evidence still within its minimum retention is kept unless overridden.
5. Destroy the key-encryption key when no backup must be readable any more.

## Transition support

TO BE COMPLETED: the vendor's commitments during an exit (notice periods,
assistance, duration), to be set in the commercial terms.
