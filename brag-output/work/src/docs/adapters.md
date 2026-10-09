# Adapter developer guide

An adapter moves data from a system you own (an inventory, a CMDB, a vendor
register, a spreadsheet export) into the compliance engine. It reads the
source and produces **batches of canonical records**. The engine validates
them, versions them as snapshots and evaluates them against control catalogs.

The contract is language-neutral. You can write an adapter in any language:

| Part of the contract | Where |
|---|---|
| Manifest format | `schema/manifest.schema.json` (JSON Schema 2020-12) |
| Batch envelope and canonical records | `schema/v0.1.0/schema.json` (JSON Schema, with `x-*` extensions) |
| Ingestion API | `api/openapi.yaml`, served at `/api/v1/openapi.yaml` |
| Conformance | `compliance-engine adapter test` (any language), `sdk/adaptertest` (Go) |

The Go SDK (`github.com/nexops-one/compliance-engine/sdk`) and the Python SDK
(`sdk-python`, package `compliance_engine_sdk`) are conveniences over that
contract. `examples/adapter-go` and `sdk-python/examples/vendor_adapter.py` are
the same complete adapter built on each.

## Trust boundary

An adapter only produces batches. It never receives a database or engine
handle, and it never chooses the tenant or workspace: the engine binds the
scope from the API token (or from the host application when embedded).

## 1. Manifest

The manifest declares what the adapter can supply. Register it before pushing:

```json
{
  "name": "vendor-inventory",
  "version": "1.0.0",
  "schema_version": "0.1.0",
  "supplies": {
    "ict_provider": ["provider_id_code", "provider_id_type", "legal_name", "hq_country"],
    "cloud_resource": ["resource_ref", "provider_id_code", "service_name", "region", "country"]
  },
  "modes": ["full", "incremental"]
}
```

- `name`: letters, digits, `.`, `_`, `-` (up to 128). It is the `source.adapter` of every batch.
- `version`: sent as `source.adapter_version`.
- `schema_version`: the canonical schema version you emit.
- `supplies`: entity → fields. Every supplied entity must include its identity fields
  (for example `provider_id_code` for `ict_provider`). Fields that no registered adapter
  supplies are reported as `not_supplied_by_any_adapter` in completeness: they are never defaulted.
- `modes`: `full` and/or `incremental` (see below).

## 2. Batch envelope

```json
{
  "schema_version": "0.1.0",
  "batch": { "batch_id": "vendors@2026-09-28T18:00:00Z", "mode": "full" },
  "source": { "system": "vendor-inventory-export", "adapter": "vendor-inventory", "adapter_version": "1.0.0" },
  "entities": {
    "ict_provider": [
      { "provider_id_code": "SAMPLETP000000000087", "provider_id_type": "LEI",
        "legal_name": "Nimbus Cloud Europe Ltd", "hq_country": "IE",
        "_meta": { "source_record_ref": "vendors/V1" } }
    ],
    "cloud_resource": [
      { "resource_ref": "V1/vm-001", "provider_id_code": "SAMPLETP000000000087",
        "service_name": "Compute", "region": "eu-west-1", "country": "IE",
        "_meta": { "source_record_ref": "vendors/V1/resources/vm-001",
                   "derived_fields": [ { "field": "country", "method": "region_lookup", "source_ref": "eu-west-1" } ] } }
    ]
  }
}
```

**Modes.**
- `incremental` (default): records are created or updated; nothing is deleted.
- `full`: the batch is the complete set of what this adapter supplies. Records of the
  manifest's entities that this adapter sent before and that are absent from the batch are
  deleted. A full batch is therefore authoritative only for its own adapter and its
  manifest's entities. If any record of an entity is rejected, no record of that entity is
  deleted (the batch reports `full_sync_deletions_skipped`). Send a full sync as **one**
  batch: two full batches delete each other's records.

**Identity.** Each entity has an identity key (`x-identity-key` in the schema). Records
with the same identity replace each other. Two records with the same identity in one batch
are rejected (`duplicate_identity`).

**Idempotency.** Sending identical content again creates no snapshot (`no_changes: true`).
Keep batches deterministic: no generation timestamps inside records, stable ordering of
list values, stable `_meta.source_record_ref`.

## 3. Field states

Every field of every record is in one of these states. Missing data is never a pass.

| State | How to express it |
|---|---|
| provided | the field has a value |
| missing | leave the field out (or `null`). Never send `""`, `0` or a placeholder |
| not applicable | list the field in `_meta.not_applicable_fields` (for example no parent company) |
| derived | provide the value and declare it in `_meta.derived_fields` with the method and what it was derived from |

`_meta.source_record_ref` identifies the record in your system; it appears in every error
and in provenance. Values that fail validation are reported with the entity, the record
index, the `source_record_ref` and the field.

## 4. Go SDK quickstart

```go
type VendorAdapter struct{ Path string }

func (a *VendorAdapter) Manifest() adapter.Manifest { /* see section 1 */ }

func (a *VendorAdapter) Pull(ctx context.Context, req adapter.SyncRequest) (adapter.Batch, error) {
	b := adapter.NewBuilder(a.Manifest(), "vendor-inventory-export").Mode(req.Mode).BatchID("vendors@...")
	rec := adapter.Record{"resource_ref": "V1/vm-001", "region": "eu-west-1", "country": "IE"}
	adapter.MarkDerived(rec, adapter.DerivedField{Field: "country", Method: "region_lookup", SourceRef: "eu-west-1"})
	adapter.SetSourceRef(rec, "vendors/V1/resources/vm-001")
	b.Add("cloud_resource", rec)
	return b.Build(), nil
}
```

The full example is `examples/adapter-go/vendor.go`. Useful SDK functions:
`adapter.DecodeManifest` and `adapter.DecodeBatch` (strict decoding),
`Manifest.Validate(registry)`, `Batch.ValidateEnvelope(schema)`,
`schema.Default()` (the embedded canonical schemas) and `Schema.ValidateRecord`.

### Conformance in your Go tests

```go
func TestConformance(t *testing.T) {
	derived := map[string][]string{"cloud_resource": {"country"}} // fields this input makes the adapter infer
	adaptertest.Run(t, &VendorAdapter{Path: "testdata/vendors.json"},
		adaptertest.Fixture{Name: "full", Request: adapter.SyncRequest{Mode: adapter.ModeFull}, Derived: derived},
		adaptertest.Fixture{Name: "incremental", Request: adapter.SyncRequest{Mode: adapter.ModeIncremental}, Derived: derived},
	)
}
```

`Run` pulls each fixture twice and reports each suite as a subtest.
`adaptertest.Evaluate` returns the same report as data, and `WriteText`/`WriteJUnit` print it.

### Pushing to an engine

```go
c := push.New("https://compliance.example.com", os.Getenv("COMPLIANCE_TOKEN"))
if err := c.RegisterManifest(ctx, a.Manifest()); err != nil { ... }
results, err := c.Push(ctx, batch) // one result per request
```

The client retries 429, 502, 503, 504 and network errors with exponential backoff
(`MaxAttempts`, default 4; `Backoff`, default 500 ms). Retrying is safe because the engine
records no change for content it already holds. An **incremental** batch larger than
`MaxRecords` (default 10,000) or `MaxBodyBytes` (default 16 MiB) is split into several
requests with batch IDs `<batch_id>#1`, `#2`, …. A **full** batch is never split: it fails
with `push.ErrFullBatchTooLarge` before sending anything. Raise the engine limits
(`COMPLIANCE_MAX_RECORDS`, `COMPLIANCE_MAX_BODY_BYTES`) and the client's, or sync
incrementally. Engine errors are returned as `*push.APIError` (`Status`, `Code`,
`Message`, `Details`).

### Python SDK

[sdk-python](../sdk-python/README.md) mirrors the Go SDK: `Manifest`,
`BatchBuilder`, the metadata helpers, `validate_batch` (local validation with
the same error codes, against the same schemas) and `Client` (push with retry
and chunking). The engine's test suite runs its example adapter through the
conformance runner below and checks that it produces the Go example's batch.

```bash
python sdk-python/examples/vendor_adapter.py --out out
compliance-engine adapter test --manifest out/manifest.json --batches out/batches --derived cloud_resource.country
```

## 5. Push API for other languages

```bash
TOKEN=...   # from: compliance-engine token generate --tenant t --workspace w
curl -X PUT -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  --data @manifest.json https://compliance.example.com/api/v1/adapters/vendor-inventory/manifest
curl -X POST -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  --data @batch.json 'https://compliance.example.com/api/v1/ingestions?dry_run=true'
curl -X POST -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  --data @batch.json https://compliance.example.com/api/v1/ingestions
```

| Response | Meaning |
|---|---|
| 201 | committed: `snapshot_id`, `created`/`updated`/`deleted`, `accepted`, `rejected_records`, `errors`, `warnings` |
| 200 | dry run (`{"result": ..., "completeness": ...}`), or no changes (`no_changes: true`) |
| 401 `unauthorized` | missing or unknown token |
| 403 `feature_not_entitled` | the edition or license does not include ingestion |
| 413 `payload_too_large`, `too_many_records` | over the body or record limit: split incremental batches |
| 422 `invalid_manifest`, `name_mismatch`, `unknown_adapter`, `mode_not_supported`, `unsupported_schema_version`, `invalid_batch` | contract errors, with `details` for field-level problems |

Rejected **records** do not fail the request: they are listed in `errors` with their
location, and the accepted records are committed. Errors always have the shape
`{"error": {"code": "...", "message": "...", "details": [...]}}`.

## 6. Conformance runner (any language)

Produce your manifest and the batches of one sync as files, then:

```bash
compliance-engine adapter test --manifest manifest.json --batches out/run1 \
  --repeat out/run2 --derived cloud_resource.country --junit conformance.xml
```

- `--batches`: a batch file, or a directory whose `*.json` files are the batches of one sync.
- `--repeat`: the batches of a second sync **of the same input** (enables the idempotency check).
- `--derived`: `entity.field` values your input makes the adapter infer.
- `--junit`: also write JUnit XML for CI. Exit code 0 = pass (warnings allowed), 1 = fail, 2 = usage.

Or test the delivery path itself: run a local endpoint that implements the manifest and
ingestion routes (unauthenticated, nothing stored), point your adapter at it, push one
sync, then stop it with Ctrl+C to get the report:

```bash
compliance-engine adapter test --listen 127.0.0.1:9090 --derived cloud_resource.country
```

Idempotency is not checked in listen mode; use `--repeat` with files.

### Checks

| Check | Fails when |
|---|---|
| `manifest` | the manifest violates the manifest JSON Schema, names an unknown entity or field, or omits an identity field of a supplied entity |
| `pull` | (Go harness) `Pull` returns an error or a batch in another mode than requested; (listen mode) nothing was pushed |
| `envelope` | the file is not a valid batch, `schema_version` is unsupported, or the envelope is invalid |
| `manifest_match` | `source.adapter`/`source.adapter_version` differ from the manifest, the schema version differs, or the mode is not declared |
| `records` | a record fails structural (L1) validation |
| `identity` | two records of one entity in one batch have the same identity key |
| `outside_manifest` | an entity or field is emitted that the manifest does not supply |
| `derived_fields` | a field that the input makes the adapter infer is sent without a `_meta.derived_fields` entry |
| `full_scope` | one sync sends more than one full batch. **Warning** (does not fail): a full batch omits an entity the manifest supplies, which would delete all of its records |
| `idempotency` | the second sync of the same input would create, update or delete anything |

The Go harness and the CLI runner share these checks (`sdk/adaptertest`), so they give the
same verdict on the same batches.

## 7. Versions

The SDK module is versioned independently of the engine (`sdk/vX.Y.Z` tags). An SDK
release embeds the canonical schema versions it supports (`schema.Default().Versions()`).
A batch declaring any patch of a supported `MAJOR.MINOR` is validated with the highest
loaded patch of it.

Outputs of the engine are operational readiness aids, not legal advice and not certification.
