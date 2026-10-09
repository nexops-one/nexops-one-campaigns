# compliance-engine Python SDK

`compliance_engine_sdk` builds canonical batches, validates them locally
against the canonical schemas, and pushes them to a compliance engine. It
mirrors the Go SDK (`github.com/nexops-one/compliance-engine/sdk`).

- Python 3.9 or later; one dependency, `jsonschema`.
- Apache-2.0 ([LICENSE](LICENSE)). Versioned independently of the engine: tags `sdk-python/vX.Y.Z`.

```bash
pip install ./sdk-python            # from the engine repository (not published to PyPI yet)
```

## Build a batch

```python
from compliance_engine_sdk import BatchBuilder, Manifest, Mode, Registry, mark_derived, set_source_ref, validate_batch

manifest = Manifest(
    name="vendor-inventory", version="1.0.0", schema_version="0.1.0",
    supplies={"ict_provider": ["provider_id_code", "legal_name", "hq_country"]},
    modes=[Mode.FULL, Mode.INCREMENTAL],
)
registry = Registry.default()
manifest.validate(registry)          # unknown entities or fields, missing identity fields, bad modes

record = {"provider_id_code": "529900T8BM49AURSDO55", "legal_name": "Example Cloud Ltd"}
set_source_ref(record, "vendors/42")                      # the record's ID in your system
mark_derived(record, "hq_country", "registry_lookup")     # when a value is inferred, say so
record["hq_country"] = "IE"

batch = BatchBuilder(manifest, "vendor-export").mode(Mode.FULL).batch_id("2026-10-02").add("ict_provider", record).build()
for e in validate_batch(batch, registry):                 # envelope and records (L1), before sending
    print(e.entity, e.index, e.field, e.code, e.message)
```

Leave a value out when the source does not have it: absent means missing,
never an empty string or a default. Use `mark_not_applicable(record, field)`
when a field does not apply to a record.

Validation reports the same codes as the Go SDK and the engine: `required`,
`unknown_field`, otherwise the failing keyword (`type`, `pattern`, `enum`,
`format`, `minLength`, ...); fields are dotted paths. The engine runs more
checks on ingestion (identifier check digits, references, completeness).

## Push

```python
from compliance_engine_sdk import Client

client = Client("https://compliance.example.com", token)   # the token's scope selects the tenant and workspace
client.register_manifest(manifest)
for result in client.push(batch):
    print(result.ingestion_id, result.accepted, result.rejected_records, result.snapshot_id)
```

- Retries on 429, 502, 503, 504 and connection errors, 4 attempts, backoff 0.5 s doubling.
- An incremental batch larger than 10,000 records or 16 MiB is split, in entity then record order, and each part's batch ID gets `#<n>`.
- A full batch is never split (each part would delete what the others supplied): `FullBatchTooLarge`. Raise the limits on both sides or sync incrementally.
- Errors: `APIError` (`status`, `code`, `message`, `details`); on a failure after some parts were committed, the error's `results` lists them.

## Conformance

Check your adapter's output with the engine's language-neutral runner:

```bash
python my_adapter.py --out run1 && python my_adapter.py --out run2
compliance-engine adapter test --manifest run1/manifest.json --batches run1/batches --repeat run2/batches
```

[examples/vendor_adapter.py](examples/vendor_adapter.py) is a complete adapter;
the engine's test suite runs this check on it.

## Development

```bash
python -m pytest sdk-python          # from the repository root (needs jsonschema and pytest)
go generate ./schema                 # after changing a canonical schema: refreshes compliance_engine_sdk/schemas
```

`sdk/schema/testdata/validation-cases.json` is checked by both the Go and the
Python test suites, so the two SDKs keep reporting the same errors.
