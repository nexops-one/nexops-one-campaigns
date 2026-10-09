# Running the standalone server

The `compliance-engine` binary serves the REST API, manages the PostgreSQL
schema and validates data offline. It makes no outbound network calls and
sends no telemetry.

## Quick start with Docker Compose

```bash
cd deploy
cp .env.example .env
go run ../cmd/compliance-engine token generate --tenant acme --workspace register-2026
# put the printed "COMPLIANCE_TOKENS entry" in .env, keep the token for API clients
docker compose up -d --build
curl -H "Authorization: Bearer <token>" http://127.0.0.1:8080/api/v1/about
```

The engine applies database migrations on start (`COMPLIANCE_AUTO_MIGRATE=true`).
`deploy/smoke.sh` runs the whole flow end to end.

## Configuration

All configuration comes from `COMPLIANCE_*` environment variables. The full
list, with defaults, is generated from the code:
[reference/configuration.md](reference/configuration.md).

## API tokens, users and roles

Each token is bound to exactly one tenant workspace and a set of roles. Every
request made with it reads and writes only that workspace, and only what its
roles allow. The server stores only SHA-256 hashes of tokens. Roles, permissions,
user and service tokens, and the audit log are described in [access.md](access.md).

**Static tokens** live in `COMPLIANCE_TOKENS` (bootstrap, automation):

```bash
compliance-engine token generate --tenant acme --workspace register-2026            # owner+admin
compliance-engine token generate --tenant acme --workspace register-2026 --role auditor
compliance-engine token hash <existing-token>
```

To revoke a static token, remove its entry from `COMPLIANCE_TOKENS` and restart.

**Stored tokens** (PostgreSQL) are created, listed and revoked without a restart,
by the CLI or through `/api/v1/tokens`:

```bash
compliance-engine user create --tenant acme --email ann@example.com --workspace register-2026 --role owner+approver
compliance-engine token create --tenant acme --workspace register-2026 --name "ann laptop" --role approver --email ann@example.com --expires 90d
compliance-engine token create --tenant acme --workspace register-2026 --name "erp sync" --role owner --service
compliance-engine token list   --tenant acme --workspace register-2026
compliance-engine token revoke --tenant acme --workspace register-2026 tok-...
compliance-engine user reset-password --tenant acme --email ann@example.com
compliance-engine audit verify --tenant acme
```

Passwords and token values are printed once. Send tokens as `Authorization: Bearer <token>`.

## API

The OpenAPI definition is served at `/api/v1/openapi.yaml` (source:
`api/openapi.yaml`). Main endpoints:

| Method and path | Purpose |
|---|---|
| `PUT /api/v1/adapters/{name}/manifest` | Register what an adapter can supply |
| `POST /api/v1/ingestions?dry_run=true` | Validate a batch without committing |
| `POST /api/v1/ingestions` | Ingest a batch (201 new snapshot, 200 no changes) |
| `POST /api/v1/imports?dry_run=true` | Validate a CSV or XLSX file (multipart field `file`) and show the resulting completeness |
| `POST /api/v1/imports` | Import a CSV or XLSX file (`mode=full` to replace what earlier imports of the same kind supplied) |
| `GET /api/v1/templates.xlsx`, `GET /api/v1/templates/{entity}.csv` | Import templates generated from the canonical schema |
| `POST /api/v1/ingestions/{id}/rollback` | Restore the snapshot before an ingestion |
| `GET /api/v1/snapshots/{id}/completeness` | Register gaps, codelists, dangling references (`{id}` may be `current`) |
| `GET /api/v1/snapshots/{id}/records/{entity}` | Records with per-field states |
| `POST /api/v1/evaluations` | Evaluate against catalogs; score is always returned with coverage |
| `POST /api/v1/reports` | Generate a report (Profile B: readiness and evidence, JSON and PDF); see [reports.md](reports.md) |
| `GET /api/v1/reports/{id}/files/{name}` | Download `report.json` or `report.pdf` |
| `POST /api/v1/reports/{id}/regenerate` | Verify that a report still follows from its inputs |

Errors use one shape: `{"error": {"code": "...", "message": "...", "details": [...]}}`.

## Workspaces

Workspaces are registered on first use: the first write to a tenant and
workspace (an ingestion, an import, evidence, a workflow action, a setting, a
new member or token) records it, in the `_system` audit chain. Tenant IDs
starting with `_` are reserved.

A registered workspace can be **suspended** (by an edition's system
administration, or `compliance.Engine.SuspendWorkspace` in a host): its writes
are refused with `403 workspace_suspended`, while reads, report downloads,
regeneration and audit verification keep working. An edition's license can
bound the number of active workspaces: a new workspace beyond it is refused
with `403 workspace_limit_reached` (the open core has no limit). Suspending a
workspace frees a slot.

## Importing spreadsheets

Templates are generated from the canonical schema: one sheet (or CSV file) per entity,
one column per field, a README sheet describing every column, and text-formatted cells so
that codes such as `0001` keep their leading zeros.

```bash
compliance-engine templates --out ./templates          # <entity>.csv files and compliance-templates-0.1.0.xlsx
compliance-engine import --dry-run ./ict_provider.csv  # report only, no database needed
export COMPLIANCE_DATABASE_URL=postgres://...
compliance-engine import --tenant acme --workspace main ./register.xlsx
compliance-engine import rollback --tenant acme --workspace main <ingestion-id>
```

Over the API: `curl -H "Authorization: Bearer $TOKEN" -F file=@register.xlsx 'http://localhost:8080/api/v1/imports?dry_run=true'`.

- **CSV:** one entity per file, named `<entity>.csv` (or pass `--entity` / `entity=`). UTF-8
  (in Excel, save as "CSV UTF-8"; other encodings are rejected with `invalid_encoding`), with
  or without BOM, `,` or `;` separators. Workbooks may expand to at most 256 MiB (`file_too_large`).
- **Columns:** field names as in the templates. `_not_applicable` lists fields that do not
  apply to the row; `_source_record_ref` overrides the row reference. Unknown or duplicate
  columns reject the whole file (`invalid_file`).
- **Rows:** every row is validated. Bad rows are reported as `<entity>:<row>` with the field
  and the reason; good rows are imported. A value that cannot be converted (for example
  `many` in a number column) is kept as text and rejected, never dropped or defaulted.
- **Modes:** incremental by default. `--mode full` replaces what earlier imports of the
  same kind supplied: a CSV import only for its entity, a workbook import for every entity
  it supplies. If a row of an entity is rejected, nothing of that entity is deleted. Run a
  dry run first: it shows `would create, update, delete`.
- **Re-import:** importing the same file again (even renamed) changes nothing.
- **Rollback:** `import rollback <ingestion-id>` restores the previous snapshot.

## Database migrations and rollback

```bash
export COMPLIANCE_DATABASE_URL=postgres://...
compliance-engine migrate status
compliance-engine migrate up
compliance-engine migrate down --yes 1
```

The audit-log migration refuses to go down while the log holds events; add
`--force-drop-audit` only when you intend to discard the audit history (export it
first with `GET /api/v1/audit`).

Every migration has a tested down script. Before upgrading, back up the database
(`pg_dump`). To roll back an upgrade: stop the new engine, run `migrate down --yes <n>`
with the **new** release (it knows the newer migrations), then start the previous
release. `migrate down` removes the tables a migration created, including their data.

## Offline tools

```bash
compliance-engine validate [--json] batch.json       # L1/L1b checks and completeness, no server needed
compliance-engine catalog validate [dir]            # validate catalogs
compliance-engine catalog diff dora@1.0.0 dora@1.1.0 --dir ./my-catalogs
```

Outputs are operational readiness aids, not legal advice and not certification.

## Keys, retention and tenant deletion

```bash
compliance-engine keys generate --out /etc/compliance/kek        # then set COMPLIANCE_ENCRYPTION_KEY_FILE
compliance-engine keys seal-existing --tenant acme               # after enabling encryption on existing data
compliance-engine keys rotate --from OLD --to NEW                # new key-encryption key
compliance-engine keys rotate --data --tenant acme               # new data key version for a tenant
compliance-engine retention run --dry-run                        # what the retention policies would delete
compliance-engine tenant delete --tenant acme --yes              # delete a tenant and destroy its keys
```

See [security.md](security.md) for what each command protects or deletes.
