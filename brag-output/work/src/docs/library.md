# Embedding the compliance engine

The engine is a Go library (`github.com/nexops-one/compliance-engine/pkg/compliance`).
A host creates one `Engine` and passes a `Scope` (tenant and workspace) on every
call. The scope always comes from the host's own authentication, never from data.

```go
eng, err := compliance.New(ctx, compliance.Config{Store: memory.New()})
scope := compliance.Scope{TenantID: "acme", WorkspaceID: "register-2026"}

// 1. Register what the adapter can supply.
err = eng.RegisterManifest(ctx, scope, manifest)

// 2. Validate without committing, then ingest.
preview, err := eng.DryRun(ctx, scope, batch)   // result + completeness view
result, err := eng.Ingest(ctx, scope, batch)    // new snapshot rev-N, or no change

// 3. Inspect gaps and evaluate.
gaps, err := eng.Completeness(ctx, scope, "")   // "" = current snapshot
ev, err := eng.Evaluate(ctx, scope, "", nil)    // nil = latest catalogs

// 4. Undo an ingestion (creates a new revision; history is kept).
rb, err := eng.Rollback(ctx, scope, result.IngestionID)
```

## Honesty rules

- A control is `not_assessed` when a required field is missing, derived without
  confirmation, or references a record that does not exist, when its rule applies
  to no records, or when it needs a human assessment. Missing data never passes.
- The engine produces `monitoring` (inputs complete, rule satisfied) and `in_review`
  (inputs complete, rule not satisfied). `ready` requires human approval.
- Score and coverage are always reported together:
  `score = (ready + monitoring) / assessable`, `coverage = assessable / in scope`.

## Configuration

| Field | Default | Purpose |
|---|---|---|
| `Store` | required | `memory.New()` here; Postgres in a later release |
| `Catalogs` | embedded DORA, GDPR, EU AI Act | replaces the set; use `catalog.Sources{catalog.Embedded(), extra}` to add |
| `Entitlements` | `extension.AllowOpen{}` | gates writes and evaluation per feature; reads are never gated |
| `IdentifierPolicy` | `warn` | LEI, country and currency check failures warn or reject |
| `Limits.MaxRecordsPerBatch` | 50000 | batches above the limit fail with `ErrTooLarge` |
| `Version` | `dev` | engine version recorded in reports |
| `Extensions.ReportProfiles` | none | report profiles besides the built-in Profile B (see [reports.md](reports.md)) |

## Writing an adapter

Use the SDK module `github.com/nexops-one/compliance-engine/sdk`: declare an
`adapter.Manifest`, build batches with `adapter.NewBuilder`, mark inferred values
with `adapter.MarkDerived` and fields that do not apply with
`adapter.MarkNotApplicable`, and validate locally with `schema.Default()` and
`Schema.ValidateRecord`. `Engine.Sync` runs an `adapter.Adapter` in process.

Outputs are operational readiness aids, not legal advice and not certification.
All regulatory references in the shipped catalogs are indicative pending legal review.
