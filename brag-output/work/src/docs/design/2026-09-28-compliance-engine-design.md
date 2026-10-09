# compliance-engine: Core Engine Foundation (Design)

**Status:** Approved in brainstorming, pending written-spec review
**Date:** 2026-09-28
**Implements:** compliance-service specification v2 (`docs/compliance-service-specs-v2.md`), cycle 1
**Canonical model:** `docs/api/canonical-data-model-v0.1.md` and `docs/api/canonical-data-model.schema.json`
**Covers:** SPEC-INT-001, SPEC-INT-002, SPEC-INT-003, SPEC-GOV-001, SPEC-GOV-004, parts of SPEC-DEP-001 (milestones M9 and M10)

---

## 1. Goal and understanding

Rebuild the compliance service as a **data-source-agnostic compliance engine** that:

- runs **standalone** (self-hosted, no dependency on NEXOPS ONE), and
- is **embeddable as a Go library** in NEXOPS ONE, which becomes its reference integration.

This first cycle delivers the engine foundation: canonical model with validation, provenance and snapshots; adapter contract and SDK; data-driven versioned control catalogs; evaluation with score and coverage; CSV/XLSX import; Postgres and in-memory storage; a standalone server and CLI; and a thin NEXOPS ONE host that keeps the current web app working.

### Decisions taken

| Topic | Decision |
|---|---|
| First slice | Core engine foundation (M9 + M10 core). Evidence, workflow, security baseline, reports and open-core release follow in later cycles. |
| Repository | New standalone repo at `C:\Users\nephty\PROJECTS\compliance-engine`, module `github.com/nexops-one/compliance-engine`, local `git init`, no remote push. |
| Monorepo side | `apps/compliance-service` becomes a thin host that embeds the engine, provides the NEXOPS ONE reference adapter, and serves a compatibility shim. |
| HTTP API | New versioned API (`/api/v1`, OpenAPI) in the engine; the legacy `/v1/compliance/*` routes stay as a shim in the NEXOPS host until `apps/web` migrates. |
| Storage | Postgres (system of record, ADR-005) and an in-memory store (tests, embedding, demo). |
| Model representation | **Schema-driven generic records**: the published JSON Schema is the single source of truth; no hand-written per-entity Go structs. |
| Failed rules | A control whose inputs are complete but whose rule is not satisfied gets `in_review` ("needs attention"), not a new state and never `ready`. |
| Commercial model | Open core sold to financial entities; also a paid add-on inside NEXOPS ONE. Two editions built from two repositories: open `compliance-engine` and private `compliance-engine-enterprise`, which plugs in through public extension points (Section 11). |
| NEXOPS ONE gating | The M8 compliance summary and score stay free for every workspace. Register data, import, completeness, catalogs, evaluations, and later workflow and reports require the compliance add-on. |
| Licensing | Offline Ed25519-signed license file with a grace period; data is never locked. The `Entitlements` interface ships in cycle 1; the license provider ships in the commercial cycle. |
| SDK | Language-neutral contract (JSON Schema, batch envelope, manifest, OpenAPI). Cycle 1 ships a lightweight Go SDK module and a CLI conformance runner usable from any language. Python SDK later. |

### Assumptions

- The engine stays in Go (1.24), consistent with the monorepo.
- Codelist values and authority validation rules are not yet loaded. Until they are, codelist checks report "codelist unverified" and never pass silently.
- All regulatory references in catalogs are **indicative**, pending the legal review listed in spec v2 §12.

### Out of scope for this cycle

- Evidence lifecycle, review workflow, roles and approvals (SPEC-GOV-002, SPEC-GOV-003; M11)
- Encryption at rest, append-only audit log, retention (SPEC-SEC-001/002; M11)
- Report profiles A and B (SPEC-GOV-005; M12)
- API token management UI, SSO, license choice, contribution policy, demo dataset (M13)
- Helm chart (compose only in this cycle)
- Commercial cycle: the `compliance-engine-enterprise` repository, the license-file entitlements provider and signing tool, the private image registry, commercial catalogs and profiles
- Python SDK
- Vendor due-diligence pack (the vendor's own register data, exit plan, subprocessors)

---

## 2. Repositories and packages

### 2.1 New repository `compliance-engine`

```text
compliance-engine/
  schema/v0.1.0/schema.json        canonical JSON Schema (copied from the monorepo docs), embedded
  schema/v0.1.0/codelists/         versioned codelist files (empty placeholders until official lists arrive)
  catalogs/dora/1.0.0.yaml         control catalogs (data)
  catalogs/gdpr/1.0.0.yaml
  catalogs/aiact/1.0.0.yaml
  catalogs/meta-schema.json        schema that every catalog file must satisfy
  sdk/              SEPARATE Go module github.com/nexops-one/compliance-engine/sdk (stdlib + jsonschema only):
    sdk/adapter       Manifest, Adapter interface, Mode, SyncRequest, Batch builder, local validation
    sdk/adaptertest   conformance suite (Go test harness)
    sdk/push          HTTP client that pushes batches and manifests to an engine's /api/v1
    sdk/schema        embedded canonical schemas (the schema/ directory is the source; copied by `go generate`, checked in CI)
  pkg/canonical     schema registry, Record, Batch envelope, L1/L1b validation, L2/L3 analysis, field states
  pkg/extension     public extension points: CatalogSource, ReportProfile, Authenticator, AdapterFactory, Entitlements
  pkg/catalog       catalog load, validate, diff, version selection
  pkg/engine        evaluation, score and coverage, explanations (pure, no I/O)
  pkg/store         store interfaces, shared contract test suite
  pkg/store/memory  in-memory implementation
  pkg/store/postgres Postgres implementation, embedded migrations (up and down)
  pkg/importer      CSV/XLSX templates, dry-run validation report, commit, rollback
  pkg/compliance    library facade: the embeddable public API
  pkg/httpapi       versioned REST handlers, mountable http.Handler
  api/openapi.yaml  OpenAPI definition of /api/v1
  cmd/compliance-engine  open-core binary: serve, import, validate, catalog, migrate, adapter test, docs gen
  examples/adapter-go    example adapter using only the SDK, passing the conformance suite
  deploy/Dockerfile, deploy/docker-compose.yml   engine + Postgres
  deploy/offline/        script producing the air-gapped bundle
  docs/             documentation set (Section 11.4), design (copy of this spec)
  LICENSE-PENDING.md     placeholder stating the license decision is open (spec v2 SPEC-LIC-001)
```

### 2.2 Dependency rules

- The engine imports nothing from NEXOPS ONE.
- `pkg/canonical`, `pkg/catalog` and `pkg/engine` perform no I/O beyond reading embedded or caller-supplied bytes.
- Only `pkg/store/postgres` imports pgx.
- `pkg/httpapi` depends only on `pkg/compliance`.
- The `sdk` module never imports the engine module; the engine imports the `sdk` module (it reuses `Manifest`, `Mode` and the batch types, so there is one definition of the contract).
- The open repository contains no license-checking code and no proprietary component. Commercial features are added only by the enterprise repository through `pkg/extension`.
- Third-party libraries are limited to permissive licenses: `santhosh-tekuri/jsonschema/v6` (JSON Schema 2020-12), `xuri/excelize/v2` (XLSX), `gopkg.in/yaml.v3` (catalogs), `jackc/pgx/v5` (Postgres).

### 2.3 NEXOPS ONE monorepo

```text
apps/compliance-service/
  cmd/compliance-service/main.go   embeds pkg/compliance, wires store, adapter, shim
  nexopsadapter/                   reference adapter (inventory + sovereignty over HTTP -> canonical batches)
  legacy/                          M8 assessments and evidence tables and handlers, kept until M11
  shim/                            /v1/compliance/* routes mapped onto the engine facade
```

- `go.work` gains `use ../compliance-engine` for local development. `go.mod` requires the module and switches to a tagged version once one exists.
- The current `compliance.go`, `storage.go` and `canonical/` package are removed. The uncommitted normalization logic in `canonical/schema.go` is ported into `nexopsadapter` and reworked to emit v0.1 canonical records.
- Until the engine has a published tag, `apps/compliance-service/go.mod` carries `replace github.com/nexops-one/compliance-engine => ../../../compliance-engine`, and the host's `vendor/` directory (produced by `go mod vendor`) is committed so the Docker build (monorepo root context, `GOWORK=off`) works without the sibling repo. The replace and vendor directory are removed once a tag exists.
- The gateway keeps proxying `/v1/compliance` unchanged.

---

## 3. Canonical model handling (`pkg/canonical`)

### 3.1 Schema registry

- Each supported canonical version is an embedded JSON Schema. The registry loads the current version and at least one previous minor version.
- From the schema's extensions the registry derives an **entity descriptor** per entity: identity key (`x-identity-key`), per-field references (`x-references`), export-required flag (`x-roi-required`), codelist name (`x-codelist`), register reference (`x-roi-ref`), and template (`x-roi-template`).
- Field paths are written `entity.field` everywhere (catalogs, gaps, explanations).

### 3.2 Records and batches

- A `Record` is a validated JSON object for one entity plus its optional `_meta` (`source_record_ref`, `not_applicable_fields`, `derived_fields`).
- A `Batch` follows the envelope in the model doc: `schema_version`, `source {system, adapter, adapter_version}`, optional `batch {batch_id, generated_at, mode}`, and `entities`.
- Records are identified by `(entity, identity key values)`. The engine assigns a stable internal ID per identity; adapters never see internal IDs.

### 3.3 Validation levels

| Level | Where | Effect |
|---|---|---|
| L1 structural | per record at ingestion | invalid record rejected with field-level errors; other records in the batch still commit |
| L1b identifiers | per record at ingestion | LEI check digits (ISO 17442 mod 97), ISO 3166-1 and ISO 4217 membership; `reject` or `warn`, configurable |
| L2 referential | over a snapshot | dangling references and cycles (`parent_lei`, `parent_id_code`, `overarching_arrangement_ref`) reported; never block ingestion |
| L3 completeness | over a snapshot | export-required fields present, codelist membership; feeds the data-completeness view |

L4 (authority rules) belongs to Profile A in a later cycle.

### 3.4 Field states

Every field of every record resolves to one of:

- `provided`
- `missing` (absent or null)
- `not_applicable` (listed in `_meta.not_applicable_fields`)
- `derived` (provided and listed in `_meta.derived_fields`; carries method and source reference)

Missing and not-applicable are never conflated. Derived values count as provided unless a consumer (a control, or completeness) asks for confirmed values only.

---

## 4. Data flow

### 4.1 Scope

Every operation takes a `Scope{TenantID, WorkspaceID}` supplied by the host or the authenticator. The engine never infers it, and every stored key includes it.

### 4.2 Ingest

1. Check the batch `schema_version` is supported.
2. Check the adapter has a registered manifest. For full syncs, the manifest also defines which entities and fields that sync covers.
3. Validate each record (L1, L1b).
4. Compute changes against the current revision:
   - `incremental`: upsert accepted records.
   - `full`: upsert accepted records, and delete records last written by the same source, inside its manifest scope, that are absent from the batch.
   - Multiple sources: last write wins at record level; history stays in the provenance log.
5. Append one provenance entry per change: `ingestion_id, entity, key, op (create|update|delete), source system, adapter@version, source_record_ref, ingested_at, content hash`.
6. Commit a new revision (Section 4.3) and return an **ingestion result**: accepted, rejected (with field errors), warnings, changes by op, new snapshot ID.

`DryRun` executes steps 1 to 4 without committing and returns the same result shape plus the L2/L3 analysis of the would-be snapshot.

Re-ingesting identical content produces no changes and no new revision.

### 4.3 Snapshots and rollback

- Each committed ingestion creates an immutable **workspace revision**. The snapshot ID identifies that revision.
- Record versions are stored copy-on-write: a revision references record versions, and unchanged records are shared between revisions.
- **Rollback** of an ingestion creates a new revision whose content equals the revision before that ingestion. History is never rewritten; the rollback is itself recorded in the provenance log.

### 4.4 Snapshot analysis

Pure functions over a snapshot:

- **L2 referential report:** each unresolved reference (record, field, target) and each detected cycle.
- **Data-completeness view:** per entity, record and field, the gaps in export-required fields, with `x-roi-ref`, field state, and whether any registered adapter manifest declares it can supply that field. This separates "no connected source supplies this field" from "a source supplies it but the value is missing".
- **Codelist checks:** values of `x-codelist` fields are checked against the loaded codelist version. Without a loaded codelist the result is `codelist_unverified`.

---

## 5. Control catalogs (`pkg/catalog`)

### 5.1 Format

```yaml
catalog: dora
version: 1.0.0
framework: DORA
jurisdiction: EU
effective_date: 2025-01-17
schema_version: "^0.1"
source_authority: "Regulation (EU) 2022/2554 (indicative, pending legal review)"
scoring:
  counts_as_ready: [ready, monitoring]
  assumptions: "Score = ready or monitoring controls / assessable controls. Coverage = assessable / in-scope controls."
controls:
  - id: dora-roi-data-location
    title: Data location of ICT services is known
    description: Country of provision, storage and processing are recorded for every ICT service line.
    source_authority: "DORA Art. 28(3); ITS RoI B_02.02 (indicative)"
    evidence_requirements:
      - Register lines with confirmed provision, storage and processing countries
    requires:
      - arrangement_service_line.country_of_provision
      - arrangement_service_line.data_at_rest_country
      - arrangement_service_line.data_processing_country
    accept_derived: false
    rule: { kind: fields_complete, entity: arrangement_service_line }
```

### 5.2 Rule kinds

A fixed, documented set. Adding a kind requires an engine release; catalogs stay pure data.

| Kind | Satisfied when |
|---|---|
| `records_exist` | the entity (optionally filtered) has at least `min` records (default 1) |
| `fields_complete` | every record of the entity (optionally filtered) has all `requires` fields of that entity provided or not applicable |
| `references_resolved` | every reference from the listed fields resolves in the snapshot |
| `field_equals` | every record in the (optionally filtered) set has `field == value` |
| `field_in` | every record in the (optionally filtered) set has `field` in `values` |

Filters are equality or membership conditions on fields of the same entity (for example `criticality_assessment in [...]`).

### 5.3 Loading, validation and diff

- Each catalog file is validated against `catalogs/meta-schema.json`, and every `requires` and rule field path must exist in the canonical schema version the catalog targets. Invalid catalogs fail at load, not at evaluation.
- Catalogs load from the embedded set and, optionally, a configured directory, so they can be updated without redeploying.
- `catalog diff <a> <b>` lists added, removed and changed controls (field-level changes).
- An evaluation records the exact catalog version it used; updating a catalog never rewrites stored evaluations.

### 5.4 Initial catalog content

- **DORA 1.0.0:** 8 to 10 controls covering the model doc §9 themes: ICT inventory, third-party visibility (ultimate parent, supply chain, signatories), data residency and location, concentration and exit readiness, resilience and criticality.
- **GDPR 1.0.0 and EU AI Act 1.0.0:** the Milestone 8 controls, re-expressed over `cloud_resource` and its `sovereignty_indicators`. No new scope (spec v2 §10 scope discipline).
- All sources are marked indicative.

---

## 6. Evaluation, score and coverage (`pkg/engine`)

`Evaluate(snapshot, catalog) -> EvaluationResult` is a pure function.

For each control:

1. **Gate.** If any `requires` field is missing on a record the rule examines, has an unresolved reference, or is derived while `accept_derived` is false, the status is **`not_assessed`**. Blockers list each field path, record key and reason (`missing`, `dangling_reference`, `derived_unconfirmed`, `not_supplied_by_any_adapter`).
2. **Rule.** Otherwise evaluate the rule:
   - satisfied: **`monitoring`**
   - not satisfied: **`in_review`**, with the failing records listed
3. `ready` is never produced automatically. It requires human approval, delivered by the M11 workflow.

Each result carries an **explanation**: control ID, catalog version, rule, input field paths, record keys examined, blockers or failing records, snapshot ID.

**Score and coverage**, per framework and overall:

- `in_scope` = number of controls in the selected catalog versions
- `assessable` = controls whose status is not `not_assessed`
- `score = (ready + monitoring) / assessable` (0 when nothing is assessable, flagged as such)
- `coverage = assessable / in_scope`

They are always returned together, are recomputable from the result list, and carry the catalog's scoring assumptions.

Evaluations are persisted as immutable records keyed by snapshot ID and catalog versions.

---

## 7. Adapter contract and SDK (`sdk` module)

The **contract is language-neutral**: the canonical JSON Schema, the batch envelope, the manifest JSON format (published as `schema/manifest.schema.json`) and the ingestion API in OpenAPI. The Go SDK is a convenience over that contract; an adapter in any language is valid if it satisfies the contract and passes conformance.

```go
type Mode string // "full" | "incremental"

type Manifest struct {
    Name          string
    Version       string
    SchemaVersion string
    Supplies      map[string][]string // entity -> fields the adapter can provide
    Modes         []Mode
}

type SyncRequest struct {
    Scope adapter.Scope // defined in the sdk; the engine aliases it as compliance.Scope
    Mode  Mode
    Since *time.Time // incremental hint
}

type Adapter interface {
    Manifest() Manifest
    Pull(ctx context.Context, req SyncRequest) (canonical.Batch, error)
}
```

- **Trust boundary:** an adapter only produces batches. It never receives a store or engine handle, and the engine binds the scope. Out-of-process adapters register their manifest and push batches through the API.
- **Batch builder:** helpers to add records by entity, set `_meta`, declare derived fields, and validate locally before sending.
- **Unsupplied fields:** fields absent from every registered manifest are reported as `not_supplied_by_any_adapter` in completeness and blockers. They are never defaulted.
- **Conformance suite** (`adaptertest.Run(t, adapter, fixtures)`) asserts:
  - every emitted batch passes L1;
  - no emitted field is outside the manifest;
  - fields flagged as inferred by the fixture are declared in `_meta.derived_fields`;
  - running the same input twice yields zero changes on the second run;
  - full-mode deletions stay inside the manifest scope.
- **Language-neutral conformance runner:** `compliance-engine adapter test --manifest m.json --batches dir/` runs the same checks as a black box on batch files produced by any adapter (the idempotency check compares two batch directories produced from the same input). `--listen :9090` instead starts a local test endpoint that accepts pushes and reports conformance on exit. Output is human-readable and JUnit XML for CI.
- **Push client (`sdk/push`):** registers a manifest and submits batches to any engine's `/api/v1` with a token, with retry and size-aware chunking below the configured limits.
- **Versioning:** the SDK module is tagged independently (`sdk/vX.Y.Z`) and declares which canonical schema versions it supports.

### 7.1 NEXOPS ONE reference adapter

- Lives in `apps/compliance-service/nexopsadapter` and uses only the public SDK.
- Runs in two modes with the same code: **embedded** (the host calls `eng.Sync`) and **connected** (it pushes batches with `sdk/push` to a customer's self-hosted engine, so compliance data stays in the customer environment while NEXOPS ONE remains a source).
- Reads inventory resources and sovereignty data over the existing internal HTTP endpoints, forwarding the caller's tenant headers.
- Emits `cloud_resource` records (with `sovereignty_indicators`) and `ict_provider` records where resolvable. Any country inferred from a region is declared as a derived field.
- Passes the conformance suite.

---

## 8. Structured import (`pkg/importer`)

- **Templates:** generated from the schema. One CSV per entity, or one XLSX workbook with one sheet per entity. Headers are the canonical snake_case field names. The XLSX includes a README sheet listing, per field, `x-roi-ref`, required-for-export, type and codelist. Served by CLI and API.
- **Parsing:** typed conversion per schema type (dates, integers, numbers, booleans). Empty cells are missing, never coerced. A reserved `_not_applicable` column holds a comma-separated field list and maps to `_meta.not_applicable_fields`.
- **Dry-run validation report:** row and field errors (L1, L1b), warnings, and missing export-required fields (L3), before anything is committed.
- **Commit:** the importer acts as the adapter `csv-import` or `xlsx-import`. The batch ID is the SHA-256 of the file, so re-importing an identical file is a no-op, and identity keys prevent duplicates in any case. Partial imports are allowed; gaps appear as `not_assessed` and in the completeness view. Default mode is `incremental`; `full` is an explicit option.
- **Rollback:** `import rollback <ingestion-id>` uses the snapshot rollback of Section 4.3.

---

## 9. Library facade and HTTP API

### 9.1 Library (`pkg/compliance`)

```go
eng, err := compliance.New(compliance.Config{
    Store:    store,            // memory or postgres
    Catalogs: catalogSource,    // extension.CatalogSource; default: embedded plus optional directory
    Entitlements: ents,         // extension.Entitlements; default: open-core AllowOpen
    Extensions: exts,           // extra ReportProfiles, AdapterFactories (enterprise edition)
    Clock:    clock,
    Limits:   limits,
    IdentifierPolicy: canonical.Warn, // or Reject
})

eng.RegisterManifest(ctx, scope, manifest)
eng.Ingest(ctx, scope, batch)       // -> IngestionResult
eng.DryRun(ctx, scope, batch)       // -> IngestionResult + analysis
eng.Sync(ctx, scope, adapter, mode) // Pull + Ingest
eng.Snapshot(ctx, scope, id)        // "" = current
eng.Rollback(ctx, scope, ingestionID)
eng.Completeness(ctx, scope, snapshotID)
eng.Evaluate(ctx, scope, snapshotID, catalogRefs)
eng.Catalogs() / eng.CatalogDiff(a, b)
```

### 9.2 HTTP (`pkg/httpapi`, `/api/v1`, documented in `api/openapi.yaml`)

| Method and path | Purpose |
|---|---|
| `POST /ingestions?dry_run=` | submit a canonical batch |
| `GET /ingestions/{id}` | ingestion result |
| `POST /ingestions/{id}/rollback` | roll back an ingestion |
| `POST /imports?dry_run=&mode=` | multipart CSV/XLSX upload |
| `GET /templates/{entity}.csv`, `GET /templates.xlsx` | import templates |
| `PUT /adapters/{name}/manifest` | register an adapter manifest |
| `GET /snapshots`, `GET /snapshots/{id}` | list and inspect revisions |
| `GET /snapshots/{id}/records/{entity}` | records with field states and provenance |
| `GET /snapshots/{id}/completeness` | data-completeness view and L2 report |
| `GET /catalogs`, `GET /catalogs/{catalog}/{version}`, `GET /catalogs/diff?a=&b=` | catalogs |
| `POST /evaluations`, `GET /evaluations/{id}` | run and fetch evaluations |

- The handler is a plain `http.Handler`, mountable in any mux.
- **Auth:** an `Authenticator` interface returns the `Scope` and actor for a request. The NEXOPS host injects its own; standalone mode ships a static-token authenticator (tokens stored as SHA-256 hashes, each bound to one tenant and workspace, configured via environment).
- Payload size and record count limits are configured and enforced; defaults are documented.
- Errors use one JSON shape with field-level details. A feature refused by `Entitlements` returns 403 with code `feature_not_entitled`, the feature name and the reason (`not_licensed`, `expired`, `addon_disabled`).
- `GET /api/v1/about` returns edition, engine version, supported schema versions, loaded catalogs and, for entitled deployments, the license's customer, features and expiry (never the key material).

---

## 10. Storage

- **Interfaces** (`pkg/store`): `RecordStore` (revisions, copy-on-write record versions, lookup by snapshot), `ProvenanceLog`, `EvaluationStore`, `ManifestStore`. Every method takes a `Scope`.
- **Memory** (`pkg/store/memory`): full implementation, used by tests, embedding and demos.
- **Postgres** (`pkg/store/postgres`): pgx pool, embedded numbered migrations with `up` and `down` scripts, `compliance-engine migrate up|down [n]`. Scope columns are part of every primary key and index.
- A shared **contract test suite** runs against both implementations. Postgres tests run when `COMPLIANCE_TEST_DATABASE_URL` is set.

---

## 11. Editions, entitlements, distribution and documentation

The standalone product is sold to financial entities on an open-core model, and is a paid add-on inside NEXOPS ONE. The customer self-hosts it, installs it from documentation, and connects its own sources through the documented adapter contract and SDK. Customers are regulated entities: the product itself will appear in their DORA register as an ICT third-party service, so it must be distributed and documented to the standard they apply to their own vendors.

### 11.1 Editions and extension points

| Edition | Built from | Contents |
|---|---|---|
| Open core | `compliance-engine` (public) | Everything in this spec: schema, SDK, engine, scoring and coverage, CSV/XLSX import, basic catalogs, memory and Postgres stores, standalone server and CLI |
| Commercial | `compliance-engine-enterprise` (private, commercial cycle) | Imports the open engine; adds maintained catalogs and their update feed, Profile A, managed adapters, SSO, advanced workflow, multi-tenant management, license provider |
| NEXOPS ONE add-on | NEXOPS host embedding the open engine (plus enterprise components once they exist) | Same engine, entitlements from NEXOPS ONE |

Public extension points in `pkg/extension`, defined in this cycle so that the enterprise edition never needs to fork or patch the open repository:

```go
type CatalogSource interface { List(ctx) ([]catalog.Ref, error); Load(ctx, ref) (catalog.Catalog, error) }
type ReportProfile interface { ID() string; Feature() Feature; /* Generate: defined with GOV-005 */ }
type Authenticator interface { Authenticate(r *http.Request) (Principal, error) } // Principal carries Scope and actor
type AdapterFactory interface { Manifest() adapter.Manifest; Feature() Feature; New(cfg json.RawMessage) (adapter.Adapter, error) }
type Entitlements interface { Allowed(ctx context.Context, scope Scope, f Feature) Decision }
```

`Feature` is a stable string identifier (for example `register.import`, `catalog.maintained`, `report.profile_a`). `Decision` carries `Allowed bool`, `Reason` and, when a grace period applies, `GraceUntil`.

### 11.2 Entitlements

- **Open core:** `AllowOpen` allows every open-core feature and denies every commercial feature with reason `not_licensed`. It contains no license-checking logic.
- **Commercial standalone (commercial cycle):** an offline license file (JSON: customer, edition, features, workspace limit, not-before, expiry) signed with Ed25519 and verified against a public key compiled into the commercial binary. No network call. A vendor-side CLI signs licenses.
- **NEXOPS ONE:** the host implements `Entitlements` from a per-organization add-on flag (Section 12).
- **Degradation rules**, which apply to every provider:
  - reading data, listing snapshots, completeness, exporting records, and all open-core features never depend on a license;
  - an expired license enters a grace period (default 30 days) during which paid features keep working and every response carries a warning header;
  - after grace, paid features return `feature_not_entitled`; stored data, evaluations and catalogs already used remain readable;
  - no data is deleted or encrypted because of licensing.

### 11.3 Distribution

- Semantic versioning of the engine, SDK module (`sdk/vX.Y.Z`), canonical schema and catalogs, each independently; `GET /api/v1/about` reports all of them.
- Release artifacts per version: multi-arch (amd64, arm64) distroless non-root container image; static binaries; SHA-256 checksums; SBOM (SPDX, from `syft`); image and checksum signatures (`cosign`, key-based so verification works offline); release notes stating migrations and their rollback.
- **Air-gapped bundle** (`deploy/offline/`): image tarball, compose file, `.env` template, checksums, signatures, and installation guide in one archive.
- Open-core images are published to a public registry; commercial images to a private registry (commercial cycle). This cycle produces the release build and the offline bundle locally; publishing is out of scope.
- No outbound network calls or telemetry in any edition.

### 11.4 Runtime and CLI

- `compliance-engine serve` reads configuration from environment variables: listen address, TLS certificate and key (optional; otherwise terminate TLS at a reverse proxy), database URL (empty selects the memory store and logs a prominent warning that data is not persisted), catalog directory, identifier policy, limits, static tokens, license file path (ignored by the open-core binary).
- `deploy/docker-compose.yml` starts the engine and Postgres with one command, without NEXOPS ONE.
- CLI: `serve`, `migrate up|down`, `validate <batch.json>`, `import [--dry-run] [--mode] <file>`, `import rollback <id>`, `templates`, `catalog validate|diff`, `adapter test`, `token hash` (produces the hash to put in configuration), `docs gen`.

### 11.5 Documentation set

Documentation lives in the open repository under `docs/` as Markdown, versioned with the code. Generated parts are produced by `compliance-engine docs gen`, and CI fails if the committed output differs from a fresh generation.

| Document | Audience | Source | This cycle |
|---|---|---|---|
| Installation guide: requirements, compose install, air-gapped install, TLS, first token, license installation, verifying signatures | Customer operations | hand-written | yes |
| Configuration reference | Operations | generated from the config struct and its field tags | yes |
| Upgrade, migration rollback, backup and restore | Operations | hand-written; backup and restore exercised by an automated test | yes |
| Security and data-flow document: what is stored, what leaves the environment in each deployment mode (standalone, embedded, connected) | Customer security, DPO | hand-written | yes |
| Adapter developer guide: contract, manifest, batch envelope, field states, derived fields, Go SDK quickstart, push API, conformance runner for other languages | Integrators | hand-written plus `examples/adapter-go` | yes |
| Canonical field reference | Integrators, compliance teams | generated from the JSON Schema (per entity: field, type, x-roi-ref, key, export-required, codelist) | yes |
| Catalog reference: controls, required fields, rules, scoring assumptions | Compliance teams | generated from catalog files | yes |
| API reference | Integrators | `api/openapi.yaml` | yes |
| Editions and boundary (open vs commercial) | Buyers | hand-written from SPEC-LIC-001 | yes |
| Disclaimer: not legal advice, not certification | Everyone | hand-written, pending legal wording (spec v2 §12) | yes |
| Vendor due-diligence pack | Customer procurement | hand-written | later |

---

## 12. NEXOPS ONE host and compatibility shim

- `apps/compliance-service` embeds the engine with the Postgres store and mounts `/api/v1` behind its existing bearer token and `x-organization-id` / `workspaceId` scoping (organization maps to tenant).
- The NEXOPS adapter syncs on demand per workspace: before summary and report requests, and explicitly via `POST /v1/compliance/sync?workspaceId=` on the host. Unchanged inventory produces no new revision (Section 4.2).
- `/v1/compliance/*` routes keep their current paths and payload shapes for `apps/web`:
  - summary and reports come from engine evaluation, with coverage added as a new field;
  - assessments and evidence continue to use the existing M8 tables and handlers (moved to `legacy/`) and overlay owner, notes, review date and attachments onto engine results;
  - `records` routes map to engine snapshots.
- **Paid add-on gating.** The host implements `Entitlements` from a new host table `compliance_addon (organization_id, enabled, enabled_at, enabled_by, expires_at)` managed through an internal admin endpoint, until NEXOPS ONE has billing.
  - Free for every workspace (no regression): the existing M8 routes `summary`, `reports`, `reports/{framework}`, `assessments` and `evidence`.
  - Add-on required: `/api/v1/*` (register data, import, completeness, catalogs, evaluations), the `records` routes, `sync`, and connected mode. Later cycles' workflow and report profiles are gated the same way.
  - A refused request returns `feature_not_entitled`, which `apps/web` can show as an upgrade prompt.
- **Connected mode.** A NEXOPS ONE organization whose customer self-hosts the engine configures an engine URL and token in the host; the NEXOPS adapter then pushes to that engine instead of the embedded one, and compliance data is not stored in NEXOPS ONE.
- The legacy pieces are removed when the M11 workflow and evidence cycle ships and `apps/web` moves to `/api/v1`.

---

## 13. Error handling

- Validation failures are data, not errors: ingestion returns a result listing rejected records with `{entity, index, source_record_ref, field, code, message}`.
- Unsupported schema version, unknown adapter, oversize payloads and scope violations are request errors (4xx) with the same error shape.
- Store failures abort the whole ingestion; a revision is either fully committed or absent.
- Invalid catalogs fail at load and are reported with file, control ID and path.

---

## 14. Testing

- Test-driven development for every package.
- Golden tests using the sample payload from model doc §10.
- Property-style checks for identity and idempotency (re-ingesting yields no change).
- Store contract suite against memory and Postgres.
- Adapter conformance suite against the CSV importer, the XLSX importer, `examples/adapter-go` and the NEXOPS adapter; the CLI runner is tested against the same fixtures and must agree with the Go harness.
- `sdk` module tests run in isolation (`GOWORK=off`) to prove it does not depend on the engine.
- Entitlements: open-core `AllowOpen` denies commercial features; degradation rules (grace, post-grace read access) tested with a fake provider; NEXOPS host gating tested per route (free routes unaffected).
- Documentation: CI check that `docs gen` output matches the committed files; automated backup and restore test against Postgres.
- End-to-end test: import a sample spreadsheet, review the dry-run report, commit, evaluate (verify `not_assessed` controls and coverage), roll back, verify the prior snapshot is restored.
- The NEXOPS host keeps tests for the shim's payload shapes so `apps/web` does not break.

---

## 15. Acceptance mapping

| Spec criterion | Where satisfied |
|---|---|
| INT-001 machine-readable versioned schema | §3.1, embedded schema per version |
| INT-001 field-level rejection, no silent coercion | §3.3, §8 parsing |
| INT-001 provenance on every record | §4.2 step 5 |
| INT-001 previous minor version supported | §3.1 |
| INT-001 fields state requirement per control | catalog `requires` §5, `x-roi-required` §3.1 |
| INT-002 capability manifest, unsupplied fields `not_assessed` | §7, §6 blockers |
| INT-002 trust boundary, incremental and full, conformance suite, NEXOPS reference adapter | §7, §7.1 |
| INT-003 templates, validation report, idempotent, partial, rollback | §8 |
| GOV-001 versioned catalogs as data, framework version selection, required inputs, no history rewrite | §5 |
| GOV-004 score always with coverage, reproducible, documented assumptions | §6 |
| DEP-001 self-hosted without NEXOPS, env configuration, migrations with rollback, backup and restore documented and tested | §10, §11.3–11.5 |
| INT-002 documented adapter interface and SDK | §7, §11.5 adapter guide, `sdk` module, CLI conformance runner |
| LIC-001 open repo builds without proprietary components; boundary published | §2.2, §11.1, §11.5 editions document |
| SEC-002 no outbound telemetry; data-flow document | §11.3, §11.5 |
