# Reports

A report states what the engine evaluated: one snapshot of the register,
evaluated against fixed catalog versions, combined with the review workflow
at one point in time (`as_of`). Reports are stored immutably and can be
regenerated at any time to prove that they still follow from their inputs.

Reports are not legal advice and not certification. Every report says so.

## The report model

A report is built from a stored **effective evaluation** (see
[workflow.md](workflow.md)):

- Generate from a **stored evaluation** with `evaluation_id`. This is how the
  evaluation an approver reviewed becomes a report.
- Without `evaluation_id`, the engine first evaluates `snapshot_id` (default:
  the current snapshot) against `catalogs` (default: the latest version of
  every catalog) and stores that evaluation.

The report's `as_of` is always the evaluation's `as_of`. There is no free
`as_of` parameter, because the workflow state at an arbitrary past time is not
recorded.

The report is a JSON document of **facts** produced by a **profile**. Every
rendering, PDF included, is produced from that JSON only, so the JSON and the
PDF state the same facts. `facts_hash` is the SHA-256 of `report.json`, the
facts byte for byte.

Every report records:

- **Versions:** the engine version, the canonical schema version and the catalog versions;
- **Inputs:** the snapshot ID, the evaluation ID and `as_of`;
- **Generation:** the scope (tenant and workspace), the generation time and the principal who generated it;
- **Profile content:** score with coverage, limitations, and the non-certification statement.

A report generated in a workspace whose settings flag it as `sample` carries
`"sample": true` and, on every PDF page, the watermark "SAMPLE: not a
compliance status". Neither can be turned off.

## Profile B: readiness and evidence report

Profile B (`profile_b`, feature `report.profile_b`, open core) has the formats
`json` and `pdf`. Its sections are:

1. **Scope and inputs.** Tenant, workspace, snapshot, evaluation, `as_of`, catalogs, versions and the generator.
2. **Score and coverage.** Overall and per framework. A score is never stated without its coverage.
3. **Controls.** For each control: effective status, attention, stage, owner, approval expiry, and due date (with `overdue`).
4. **Control details.** For each control:
   - the description and source;
   - the rule and its inputs;
   - the number of records examined and the failing record keys;
   - blockers;
   - the evidence requirement and the linked evidence: title, kind, source, location, checksum, state and integrity.
5. **Data completeness.**
   - records, missing and derived counts per entity;
   - export-required gaps per field, split into supplied and not supplied by any adapter;
   - invalid-code counts, unverified codelists, dangling references and cycles.
6. **Limitations.** Derived from the data:
   - controls not assessed and coverage below 100%;
   - evidence that is referenced, unverified, attested, or recorded without a checksum;
   - unverified codelists and derived values;
   - each catalog's scoring assumptions;
   - the point in time.
7. **Disclaimer.**

The PDF embeds its fonts (DejaVu Sans Condensed, license in
`pkg/report/pdfdoc/fonts/LICENSE`) and refers to no external resource.
Identical facts give a byte-identical PDF.

### What a report never contains

- **No canonical field values.** Profile B shows entity names, record keys, field names and counts only, so the sensitive fields (`schema/v0.1.0/sensitive.json`) cannot appear. A test checks that no sensitive field is part of a record key.
- **Evidence locations** are reduced to scheme and host (`https://dms.example.com`, `s3://bucket`, `file`, `urn`, `managed`). Paths, queries, fragments and credentials are never shown.
- **No workflow texts:** notes, review notes, transition reasons and revocation reasons are left out.
- **No raw values in completeness:** invalid coded values are counted and their fields named, but the values are not shown.

Owners, reviewers and approvers are shown as member emails. Actors that are
not users (`system`, tokens, `migration:m8`) are shown as recorded.

## API

| Method and path | Permission | Purpose |
|---|---|---|
| `GET /api/v1/report-profiles` | `data.read` | profiles, their formats, their parameters and whether the workspace is entitled to each |
| `POST /api/v1/reports` | `report.generate` | `{profile, evaluation_id?, snapshot_id?, catalogs?, formats?, parameters?, allow_incomplete?}`; returns `201` with the report and its facts |
| `GET /api/v1/reports` | `data.read` | reports, newest first, without facts |
| `GET /api/v1/reports/{id}` | `data.read` | a report with its facts (audited `report.download`) |
| `GET /api/v1/reports/{id}/files/{name}` | `data.read` | `report.json` or `report.pdf`, byte for byte; the `Digest` header carries the SHA-256 (audited `report.download`) |
| `POST /api/v1/reports/{id}/regenerate` | `data.read` | rebuild and verify (audited `report.regenerate`) |

```bash
curl -s -X POST "$URL/api/v1/reports" -H "Authorization: Bearer $TOKEN" \
  -d '{"profile": "profile_b", "catalogs": ["dora@1.0.0"]}'
curl -s -o report.pdf "$URL/api/v1/reports/$ID/files/report.pdf" -H "Authorization: Bearer $TOKEN"
```

**Parameters.** A profile may declare report parameters (for example a
reference date), listed by `GET /api/v1/report-profiles` with a label, whether
they are required and an optional pattern. `parameters` must name declared
parameters only; values must match the pattern. The console's reports page
shows them as inputs. Parameters are stored with the report, so regeneration
reproduces it.

**Incomplete exports.** A profile that validates completeness (Profile A of the
commercial edition) refuses an export with blocking findings (`422
export_incomplete`). With `allow_incomplete: true` it produces the export
marked incomplete instead, and the report's `complete` is `false`.

Generation is gated by the profile's feature (`403 feature_not_entitled`).
Reading, downloading and regenerating existing reports are never gated, so
reports stay available when a license lapses.

### Errors

| Error | Cause |
|---|---|
| `404 not_found` | unknown profile, report or file |
| `422 invalid_parameter` | a parameter the profile does not declare, a value not matching its pattern, or one the profile refuses |
| `422 invalid_request` | a format the profile does not offer; `evaluation_id` combined with `snapshot_id` or `catalogs`; an evaluation stored before effective results existed |
| `409 stale_evaluation` | the adapter manifests changed since the evaluation, so it would not reproduce; evaluate again |
| `422 export_incomplete` | the profile refuses an incomplete export; `findings` lists why (used by Profile A) |

## Reproducibility

`POST /api/v1/reports/{id}/regenerate` does the following:

1. It recomputes the computed result from the stored snapshot, the catalog versions and the adapter supply recorded with the report.
2. It checks that this result equals the stored evaluation, and recomputes the effective result from the evaluation's stored inputs.
3. It runs the profile again on the inputs recorded with the report, such as the redacted evidence and the people.
4. It compares the facts hash and the hash of every file.

The result is `identical: true`, or a `mismatch`:

| Mismatch | Meaning |
|---|---|
| `evaluation` | the stored evaluation no longer follows from its snapshot |
| `facts` | the profile produced different facts |
| `files` | a rendering differs |

Regeneration stores nothing except its audit event. Retention keeps everything a
kept report depends on (see below).

## Storage and retention

- **Immutability:** reports cannot be changed. PostgreSQL refuses `UPDATE` on `ce_reports` and `ce_report_files`, and allows `DELETE` only for retention and tenant deletion.
- **Encryption:** with encryption at rest, report facts and files are sealed with the tenant key (see [security.md](security.md)).
- **Retention:** reports follow the workspace's `evaluation_days`. A kept report keeps its evaluation and its snapshot.
- **Tenant deletion** deletes reports.

## Writing a profile

A profile implements `extension.ReportProfile` and is registered with
`compliance.Config.Extensions.ReportProfiles`. Profile B is always registered.

```go
type ReportProfile interface {
    ID() string
    Feature() extension.Feature
    Formats() []string
    Generate(ctx context.Context, in extension.ReportInput) (extension.ReportOutput, error)
}
```

`ReportInput` carries:

- the metadata to embed (`Meta`);
- the schema and catalogs;
- the snapshot records, with values decrypted;
- the computed and effective results;
- completeness;
- linked evidence, already redacted;
- the people map;
- the requested formats;
- the report parameters and `AllowIncomplete`.

A profile that accepts parameters also implements
`extension.ParameterizedProfile` (`Parameters() []extension.ParameterSpec`). It
refuses a value its data contradicts with an error wrapping
`extension.ErrInvalidParameter`.

`ReportOutput` returns:

- the facts;
- an optional validation report;
- files;
- `Complete`, plus `Blocking` findings when the profile refuses an incomplete export.

A profile must be a pure function of its input, or regeneration reports
`facts` or `files`. It must also decide which record values it may export. The
shared helpers in `pkg/report` (`Redact`, `Hash`, `Marshal`, `Disclaimer`) and
`pkg/report/pdfdoc` (deterministic PDF with embedded fonts and the sample
watermark) are available to every profile.
