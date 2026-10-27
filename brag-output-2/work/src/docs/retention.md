# Retention and deletion

Retention deletes what a workspace's policy no longer requires, within floors
that are never crossed. Deleting a tenant removes its data and destroys its
keys. Encryption and keys are described in [security.md](security.md).

## Policy

Each workspace sets its policy on the console's Settings page (admins) or with `PUT /api/v1/settings`
(`retention: {revision_days, keep_revisions, evaluation_days}`; `0` days keep
forever). `serve` applies it every `COMPLIANCE_RETENTION_INTERVAL` (default
24 hours); `compliance-engine retention run [--dry-run]` applies it on demand.

| Data | Deleted when | Never deleted while |
|---|---|---|
| Revisions (with record versions, provenance and ingestions only they use) | older than `revision_days` | among the latest `keep_revisions`, or referenced by a kept evaluation or report; only the oldest revisions go |
| Reports (with their files) | older than `evaluation_days` | |
| Evaluations | older than `evaluation_days` | referenced by a kept report |
| Evidence items (and managed files no other item uses) | revoked or expired | within the item's `retention.min_days`, or linked to a control whose approval is current |
| Audit events | older than `COMPLIANCE_AUDIT_RETENTION_DAYS` (default 3650) | they are not the oldest of their chain; the latest event is always kept |
| Assessments and their history | never by retention | |

Every run is recorded (`retention.run`, `audit.pruned`). After pruning, the
remaining audit chain still verifies: verification trusts the first remaining
event's link to its (deleted) predecessor. In PostgreSQL the audit trigger
allows these deletions only through the prune path; updates and truncation
stay refused.

## Deleting a tenant (crypto-shredding)

```bash
compliance-engine tenant delete --tenant acme --yes
```

This deletes every record, revision, evaluation, evidence item and managed
file, assessment and history, setting, user, membership and token of the
tenant, as well as its console sessions, then **destroys its keys**. Any copy of its encrypted values (in
backups, for example) becomes unreadable, even with the KEK. The audit chains
are kept within the audit floor and record `tenant.delete` and `keys.delete`.

Evidence still within its minimum retention blocks the deletion; `--override-evidence-retention`
deletes it anyway and records that it did. Remove the tenant's static tokens
from `COMPLIANCE_TOKENS` and restart running servers, which may hold its keys in memory.

## Console sessions

Expired console sessions are deleted by `serve` every hour, independently of
the retention policy. They hold no data beyond the user, the workspace and
timestamps.
