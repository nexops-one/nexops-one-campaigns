# Backup and restore

What to back up:

| Item | Where | Notes |
|---|---|---|
| The database | PostgreSQL (`COMPLIANCE_DATABASE_URL`) | everything: records, evaluations, reports, evidence metadata, workflow, users, tokens, sessions, settings, wrapped tenant keys, audit log |
| The key-encryption key (KEK) | `COMPLIANCE_ENCRYPTION_KEY_FILE` | **separately** from the database backup, in your secret store; without it encrypted values cannot be read |
| Managed evidence files | `COMPLIANCE_EVIDENCE_STORAGE_DIR` | only when uploads are enabled; already encrypted with the tenant keys |
| Additional catalogs | `COMPLIANCE_CATALOG_DIR` | only when used |

The in-memory store (no `COMPLIANCE_DATABASE_URL`) cannot be backed up: its
data is lost when the process stops.

Back up the database with `pg_dump -Fc` (and the managed evidence directory,
if used). Restore with `pg_restore` into an empty database and start the engine
with the **same** KEK: data, evaluations and the audit chain read back as
before. With another KEK, reads of encrypted values fail with
"tenant keys are wrapped by a different key-encryption key". An automated test
(`deploy/backup_test.go`) exercises both cases against PostgreSQL.

## Step by step

```bash
# Backup (the engine may keep running: pg_dump takes a consistent snapshot)
pg_dump -Fc -d "$COMPLIANCE_DATABASE_URL" -f compliance-$(date +%F).dump
tar -czf evidence-$(date +%F).tgz -C "$COMPLIANCE_EVIDENCE_STORAGE_DIR" .   # if used

# Restore into an empty database, then start the same or a newer engine version
createdb compliance_restored
pg_restore -d compliance_restored --no-owner compliance-2026-10-01.dump
COMPLIANCE_DATABASE_URL=postgres://.../compliance_restored COMPLIANCE_ENCRYPTION_KEY_FILE=/secrets/kek compliance-engine serve
compliance-engine audit verify --tenant <tenant>   # the chains must verify after a restore
```

A newer engine version applies its migrations on start
(`COMPLIANCE_AUTO_MIGRATE=true`); see [upgrade.md](upgrade.md). Console
sessions survive a restore until they expire; sign users out by deleting the
`ce_sessions` rows if the backup is restored elsewhere.

**A backup without its KEK cannot be restored.** If the KEK was rotated
(`compliance-engine keys rotate`), restore with the KEK that was current when
the backup was taken.
