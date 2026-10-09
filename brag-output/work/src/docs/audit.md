# Audit log

Every privileged action writes an **audit event** in the same database
transaction as the action. If the event cannot be written, the action fails.
Recorded actions: `login.success`, `login.failure` (known users only, in the tenant chain) and `logout` (console sessions), `ingestion.commit` (imports included; the event names the
adapter), `ingestion.rollback`, `user.create`, `user.password_reset`, `user.email`,
`member.set`, `member.sync` (roles set by a host product's authenticator, actor `host`
unless an administrator made the change), `member.remove`, `token.create`, `token.revoke`, `evidence.create`,
`evidence.link`, `evidence.unlink`, `evidence.revoke`, `evidence.check`,
`evidence.access`, `assessment.assign`, `assessment.submit`, `assessment.recommend`,
`assessment.reject`, `assessment.approve`, `assessment.reopen`, `assessment.void`, `assessment.import`
(see [workflow.md](workflow.md)), `report.generate`, `report.download`, `report.regenerate`
(see [reports.md](reports.md)), `settings.update`, `retention.run`, `audit.pruned`,
`keys.create`, `keys.rotate_kek`, `keys.rotate_data`, `keys.delete` and `tenant.delete`
(see [security.md](security.md) and [retention.md](retention.md)). Ingestions that change nothing are not recorded. Events never contain passwords, token values or
hashes.

Each workspace has its own chain; user administration is recorded in the
tenant's chain (empty workspace), readable with the CLI. Each event carries a
sequence number, the previous event's hash and its own hash:

```text
hash = hex(SHA-256(prev_hash + "\n" + JSON{seq, tenant_id, workspace_id, at, actor,
                                           actor_kind, action, target_type, target_id, details}))
```

`at` is RFC 3339 in UTC with microsecond precision; `details` is compact JSON;
the first event of a chain has an empty `prev_hash`. Editing, deleting or
reordering any event breaks verification from that event on.

- Read: the console's Audit page, or `GET /api/v1/audit?after_seq=&limit=` (`audit.read`: auditors and admins). Both show the workspace chain; the tenant chain is read and verified with the CLI.
- Verify: the console's "Verify the whole chain" link, `GET /api/v1/audit?verify=true`, or `compliance-engine audit verify --tenant <id>` for every chain of a tenant (exit code 1 when a chain is broken).
- PostgreSQL refuses `UPDATE`, `DELETE` and `TRUNCATE` on `ce_audit_events` with a trigger. The table owner can drop triggers, so run the engine with a database role that does not own the tables in production, and keep database backups.
- `migrate down` refuses to drop the audit table while it holds events unless `--force-drop-audit` is given.

Sign-in attempts for unknown emails or tenants are logged by the server
(`console sign-in failed`) but not audited, so that unauthenticated traffic
cannot grow an audit chain. Audit events are kept at least
`COMPLIANCE_AUDIT_RETENTION_DAYS` ([retention.md](retention.md)).
