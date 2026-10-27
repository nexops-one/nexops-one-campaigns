# compliance-service: Revised Specification (v2)

**Status:** Draft for review
**Supersedes:** "7. Governance Evidence and Regulatory Workflows" (NEXOPS ONE roadmap)
**Baseline:** Milestone 8 (Compliance MVP) is complete and tested

---

## 1. Purpose and positioning

`compliance-service` is a **data-source-agnostic compliance engine**. It ingests structured data about an organization's ICT landscape (from NEXOPS ONE or any other source), evaluates it against versioned regulatory control catalogs, manages evidence and review workflows, and produces reports for management, internal audit, and national authorities.

It has two operating modes:

- **Standalone**: self-hosted by the regulated entity, which keeps full control of its sensitive data.
- **Pluggable**: embedded in or connected to another product, with NEXOPS ONE ("Cloud costs economy and sovereignty intelligence") as the reference integration.

### Design principles

1. **Customer data stays under customer control.** Self-hosting is a first-class deployment, not an afterthought.
2. **Honesty over reassurance.** Missing evidence is `not_assessed`, never a pass. This principle from Milestone 8 extends to every new feature.
3. **Explainability.** Every status, score, and report fact traces back to a control, a rule, and evidence.
4. **Source-agnostic core.** The engine knows the canonical data model, not any specific upstream system.
5. **Reproducibility.** Any report can be regenerated from a snapshot.
6. **Not legal advice, not certification.** Outputs are operational readiness and evidence aids. Reports state this explicitly.

### Non-goals (unchanged from Milestone 8, plus additions)

- Legal interpretation or formal regulatory certification
- Automated submission to authorities (export only, submission remains a human act)
- Full policy-as-code enforcement and remediation
- Complete DPIA automation and full data classification
- Continuous evidence collection from every cloud and SaaS system
- Replacing a GRC suite, contract-management system, or CMDB (the service consumes their data)

---

## 2. Scope of regulatory coverage

| Framework | Priority | Notes |
|---|---|---|
| DORA | **Primary (wedge)** | Register of information on ICT third-party arrangements first; other DORA controls follow |
| GDPR | Secondary | Controls carried over from Milestone 8 |
| EU AI Act | Secondary | Controls carried over from Milestone 8 |
| NIS2 and others | Future | Added as catalogs, no engine changes required |

**Regulatory accuracy note:** all references to authority formats, templates, and field lists in this document are **indicative** and MUST be reconciled with the current official texts and the competent authority's (CSSF) submission requirements before implementation. See Section 12.

---

## 3. Architecture overview

```text
 Adapters                     (NEXOPS ONE, CSV/XLSX, REST API, GRC / contract tools)
    |  emit canonical records
    v
 Ingestion & validation       (schema validation, provenance, gap detection)
    v
 Canonical data model         (entities, contracts, providers, services, functions, locations)
    v
 Evidence store               (checksummed, tenant-scoped, referenced not copied)
    v
 Control engine               (versioned catalogs: DORA, GDPR, AI Act; scoring; coverage)
    v
 Workflow                     (owners, reviewers, approvals, audit trail)
    v
 Report generators            (Profile A: register export; Profile B: readiness / audit)
```

Each layer communicates through documented, versioned interfaces so that any layer can be replaced or extended.

---

## 4. Integration layer

### SPEC-INT-001: Canonical data model

The service MUST define a versioned canonical schema for regulatory-relevant records. Indicative core entities:

- **Financial entity** (the reporting organization; legal identifiers, scope, group structure)
- **ICT third-party provider** (legal name, LEI or equivalent identifier, country, parent entity)
- **Contractual arrangement** (identifier, type, start and end dates, renewal and termination terms, governing law)
- **ICT service** (type, description, data-processing characteristics)
- **Supported function** (business function served, criticality or importance assessment, impact of disruption)
- **Subcontracting chain** (ordered subcontractors and the service each provides)
- **Location** (provider, data storage, and data processing locations)
- **Resilience attributes** (substitutability, exit plan reference, last audit or test date)
- **Cloud resource** (provider, region, service, sovereignty indicators; carried over from the NEXOPS ONE inventory)

Acceptance criteria:

- The schema is published as a machine-readable definition (for example JSON Schema) with a semantic version.
- Every ingested record is validated against the schema; invalid records are rejected with field-level errors and never silently coerced.
- Every record carries **provenance**: source system, adapter and version, ingestion time, and source record reference.
- Schema changes follow semantic versioning; adapters declare the schema version they target, and the service supports at least one previous minor version.
- Field definitions state whether each field is required for a given control or report profile.

### SPEC-INT-002: Adapter contract

An adapter is any component that maps an external source to the canonical model. The service MUST provide a documented adapter interface and an SDK.

Acceptance criteria:

- An adapter declares a **capability manifest**: which canonical entities and fields it can supply.
- Fields an adapter cannot supply produce `not_assessed` for dependent controls; they are never defaulted to a passing value.
- Adapters run outside the control engine's trust boundary and cannot read other tenants' data.
- Adapters support incremental updates and full re-sync, and every change is recorded in the provenance log.
- NEXOPS ONE is implemented as the **reference adapter**, using only the public adapter interface (no private access paths).
- A conformance test suite validates any adapter against the contract.

### SPEC-INT-003: Structured import (first non-NEXOPS adapter)

The service MUST support import from CSV and XLSX using documented templates, since much of the required contractual data lives in spreadsheets today.

Acceptance criteria:

- Downloadable import templates cover the register-relevant entities.
- Import produces a **validation report** (row and field errors, warnings, missing required fields) before anything is committed.
- Imports are idempotent: re-importing the same file does not duplicate records.
- Partial imports are allowed, and resulting gaps are surfaced as `not_assessed` and as an explicit data-completeness view.
- Imports can be rolled back to the prior snapshot.

### SPEC-INT-004: API ingestion

The service SHOULD expose an authenticated REST API for programmatic ingestion and retrieval, using the same validation path as file import.

Acceptance criteria:

- Endpoints are versioned and documented (OpenAPI).
- API tokens are scoped per tenant and workspace, revocable, and never returned after creation.
- Rate limits and payload size limits are documented and enforced.

---

## 5. Governance and evidence

### SPEC-GOV-001: Control catalog

Controls MUST be versioned by framework, jurisdiction, effective date, control ID, title, description, evidence requirements, source authority, and **required input fields** (the canonical fields needed to assess the control).

Acceptance criteria:

- A framework version can be selected for an assessment.
- Updating a control does not rewrite historical assessments.
- EU AI Act, DORA, and GDPR mappings are separately versioned.
- Each control declares which canonical fields it depends on, so gap detection is automatic and explainable.
- Catalogs are data, not code: they can be loaded, diffed, and validated without redeploying the engine.

### SPEC-GOV-002: Evidence lifecycle

Evidence MUST have source, kind, URI or object reference, checksum, collected time, collector, retention policy, and tenant scope.

Acceptance criteria:

- Evidence can be attached to one or more controls without copying the object.
- **Evidence content remains in the customer's environment**; the service stores references and checksums, unless the customer explicitly configures managed storage.
- Evidence integrity can be checked using its checksum, and a failed check downgrades dependent controls.
- Expired or revoked evidence is excluded from a current report and remains auditable historically.
- Evidence access is logged.

### SPEC-GOV-003: Review workflow

Assessments MUST support owner assignment, reviewer assignment, due date, status, notes, decision history, and approval or rejection reason.

Required states:

```text
not_assessed -> in_review -> monitoring -> ready
in_review -> rejected -> in_review
ready -> expired -> in_review
```

Acceptance criteria:

- Only authorized users can approve a control; role definitions are documented (at minimum: owner, reviewer, approver, read-only auditor).
- The approver MUST NOT be the same user as the control owner unless the deployment explicitly allows it.
- Every transition records actor, timestamp, previous state, and reason.
- Overdue controls are visible without changing their evidence status.
- Review dates and expiry are configurable per control.

### SPEC-GOV-004: Scoring and coverage

The score remains the percentage of controls marked `ready` or `monitoring` out of controls with available evidence (Milestone 8 model). To prevent a misleading result, it MUST always be displayed with a **coverage metric**.

Acceptance criteria:

- Coverage is defined as the percentage of in-scope controls that are assessable (not `not_assessed`).
- A score is never displayed without coverage beside it (for example "92% ready, 18% coverage").
- The score and coverage are reproducible from the displayed controls and snapshot.
- Scoring assumptions are documented and versioned with the control catalog.

### SPEC-GOV-005: Report profiles

A report MUST be generated from a fixed framework version and workspace snapshot. It MUST disclose its scope, generation time, evidence limitations, coverage, and non-certification status.

**Profile A: Register of information export (DORA).**
Produces the ICT third-party register in the format and template required by the competent authority.

- Output follows the current official template and technical format (to be confirmed, see Section 12).
- Includes a **pre-submission validation report** applying the authority's published validation rules, with errors and warnings listed per record.
- Export is blocked, or clearly marked as incomplete, when mandatory fields are missing.
- The service produces the file; submission to the authority is performed by the entity.

**Profile B: Readiness and evidence report.**
Targets management, internal audit, or an authority conversation.

Acceptance criteria (both profiles):

- PDF and JSON exports contain the same control facts.
- A report can be reproduced from its snapshot identifier.
- No secret values or raw credentials appear in an export.
- Every export embeds the catalog version, schema version, and snapshot ID.

---

## 6. Security, privacy, and tenancy

### SPEC-SEC-001: Isolation and access

- Workspace and organization boundaries MUST be preserved across ingestion, evidence, workflow, and reports (carried over from Milestone 8).
- Authentication supports SSO (OIDC/SAML) in commercial editions and local accounts in the open core.
- All privileged actions are recorded in an append-only audit log.

### SPEC-SEC-002: Data handling

- Sensitive fields (contract terms, incident details) are encrypted at rest and in transit.
- The service performs **no outbound telemetry by default**; any telemetry is opt-in and documented.
- Data retention and deletion are configurable per tenant, with deletion respecting evidence retention obligations.
- A data-flow document describes precisely what leaves the customer environment in each deployment mode.

---

## 7. Deployment

### SPEC-DEP-001: Deployment modes

| Mode | Description |
|---|---|
| Self-hosted, single tenant | Container-based deployment in the customer's environment; the primary open-core target |
| Hosted, multi-tenant | Managed offering with strict tenant isolation |
| Embedded | Library or API used inside another product, with NEXOPS ONE as the reference |

Acceptance criteria:

- Self-hosted mode installs with documented steps (container image plus compose or Helm chart) and runs without any dependency on NEXOPS ONE.
- Configuration is environment-driven and documented.
- Upgrades include database migrations with a documented rollback path.
- Backup and restore procedures are documented and tested.

---

## 8. Open-core model

### SPEC-LIC-001: Boundary between open and commercial

| Open core | Commercial |
|---|---|
| Canonical schema and adapter SDK | Authority export profiles with maintained validation rules (Profile A) |
| Control engine, scoring, coverage | Maintained, versioned regulatory catalogs and update service |
| CSV/XLSX import and basic adapters | Managed adapters (cloud providers, GRC and contract tools) |
| Evidence model and basic workflow | Advanced workflow, SSO, role management |
| Basic JSON/PDF readiness report (Profile B) | Hosted option, support, implementation services |
| CLI and single-tenant deployment | Multi-tenant management |

Acceptance criteria:

- The open-core repository builds, tests, and runs without any proprietary component.
- The license is chosen and documented before the first public release (for example Apache 2.0 for maximum adoption, or AGPL to discourage unmodified resale as a service), and the decision rationale is recorded.
- A contribution policy (CLA or DCO) is defined before accepting external contributions.
- The boundary above is published so users know what is and is not included.

---

## 9. Design-partner demo path

The standalone prototype MUST support this end-to-end demonstration without NEXOPS ONE:

1. Import a sample vendor and contract spreadsheet.
2. Review the validation report and the populated canonical model.
3. See controls with `not_assessed` states and the data-completeness view where fields are missing.
4. Assign an owner, attach evidence, and move a control through review to approval.
5. Generate the readiness report (Profile B) and, once available, the register export (Profile A) with its validation report.
6. Repeat step 1 with the NEXOPS ONE adapter to show the same engine consuming a different source.

Acceptance criteria:

- A seeded, anonymized demo dataset exists and is clearly labeled as sample data.
- The demo runs locally from a single command.
- The demo never presents sample results as a real compliance status.

---

## 10. Milestone plan

| Milestone | Deliverable | Depends on |
|---|---|---|
| **M8** (done) | Compliance MVP: control cards, score, evidence basis, `not_assessed` handling | None |
| **M9** | Canonical data model (INT-001), adapter contract and NEXOPS ONE reference adapter (INT-002), catalog versioning (GOV-001) | M8 |
| **M10** | CSV/XLSX import with validation (INT-003), coverage metric (GOV-004), self-hosted packaging (DEP-001) | M9 |
| **M11** | Evidence lifecycle (GOV-002), review workflow (GOV-003), security baseline (SEC-001/002) | M9 |
| **M12** | Report profiles: Profile B first, then Profile A register export with validation (GOV-005) | M10, M11 |
| **M13** | Open-core release: license, contribution policy, docs, API ingestion (INT-004), demo dataset | M12 |

**Sequencing rationale:** the register export (Profile A) is the strongest design-partner hook, but it depends on the data model, import, and workflow. Building those first prevents rework and keeps the honest-gap behavior consistent.

**Scope discipline:** AI Act and GDPR catalogs remain at their Milestone 8 level until DORA Profile A is validated with a real design partner.

---

## 11. Success criteria for the design-partner phase

- At least one regulated entity imports its own vendor data and gets a validation report.
- A compliance or operations professional at that entity reviews the mapping and confirms or corrects it.
- The partner can generate a draft register export they consider a credible starting point for their own submission.
- The partner can explain the value in their own words (used for pitch and case study, with permission).

---

## 12. Open questions and items to verify

These MUST be resolved before implementation of the affected specs:

1. **Register format:** confirm the current official template, field list, and technical submission format (including any xBRL-CSV requirement) and the authority's published validation rules.
2. **Authority expectations:** confirm with the CSSF's published guidance how entities submit the register and which validations apply.
3. **Legal review:** have a Luxembourg compliance or legal professional review the control mappings and disclaimers.
4. **Provider identifiers:** confirm identifier requirements (such as LEI) and how the service handles providers lacking one.
5. **Proportionality:** confirm how simplified-regime entities are treated and whether the service should model that distinction.
6. **License choice:** decide between permissive and copyleft licensing based on the intended commercial strategy.
7. **Hosting and data residency:** define where the hosted tier runs, given the sovereignty positioning of NEXOPS ONE.
8. **Liability language:** finalize the non-certification and limitation wording with legal input.

---

## 13. Traceability to Milestone 8

| Milestone 8 element | Where it lives in v2 |
|---|---|
| Explainable control cards | SPEC-GOV-001, SPEC-GOV-004 |
| Basic score with evidence source | SPEC-GOV-004 (plus coverage) |
| `not_assessed` states | SPEC-INT-002, SPEC-GOV-004, design principle 2 |
| Workspace-scoped compliance surface | SPEC-SEC-001 |
| Documentation of scoring assumptions | SPEC-GOV-004 |
| "Next step" (persisted assessments, evidence, ownership, review dates, adapters) | SPEC-GOV-002, SPEC-GOV-003, SPEC-INT-002 |
