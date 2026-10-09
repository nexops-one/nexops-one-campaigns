Status: pending legal review

# Service description

## What the product does

The compliance engine keeps a financial entity's DORA register of
information and evaluates it against control catalogs (DORA, GDPR, EU AI Act;
indicative content). It:

- receives register data from adapters (any source system) and spreadsheet imports, validated against a published canonical schema;
- evaluates controls and shows, per framework, a score always next to its coverage, with what is missing;
- keeps evidence references and runs a review workflow (owner, reviewer, approver) with an append-only, hash-chained audit log;
- produces reports: Profile B (readiness and evidence, open core) and, in the enterprise edition, Profile A (register of information export, xBRL-CSV, indicative).

Its outputs are operational readiness aids. They are not legal advice and not
a certification.

## Editions

| Edition | License | Content |
|---|---|---|
| Open core | Apache-2.0 | everything above except the commercial features |
| Enterprise | commercial (terms pending) | adds Profile A, SSO, advanced workflow, multi-tenant administration, maintained catalogs; reads never depend on the license |

See [../editions.md](../editions.md).

## Deployment modes

| Mode | Who runs it | Where customer data is |
|---|---|---|
| **Self-hosted** (binary, container image, Docker Compose, Helm chart) | the customer, on its infrastructure | the customer's PostgreSQL; the vendor has no access |
| **Offline** (air-gapped bundle) | the customer | the customer's systems; no network needed |
| **Embedded in NEXOPS ONE** | the operator of the NEXOPS ONE deployment | that deployment's PostgreSQL |
| **Connected** (NEXOPS ONE pushes to the customer's engine) | the customer runs the engine | the customer's engine; NEXOPS ONE sends inventory data to it over the API |
| **Hosted** | not offered yet | to be defined (spec v2 §12 item 7) |

In every mode offered today, the engine runs in the customer's (or the
operator's) environment. It makes no outbound call by default: no telemetry,
no update check, no license check over the network (licenses are verified
offline), no evidence fetching unless hosts are explicitly allowed.

## Components and dependencies

- One Go binary (`compliance-engine`, or `compliance-engine-enterprise`) and PostgreSQL (version 16 is the one tested; an in-memory store exists for trials).
- Third-party libraries are listed in the SBOM of each release ([sbom-and-signatures.md](sbom-and-signatures.md)); all are under permissive licenses.
- Optional: an OpenID Connect identity provider for SSO (enterprise), an evidence storage directory.

## Data the product processes

Register data (providers, contracts, services, functions, assessments),
evidence references (location and checksum; files only when managed storage is
enabled), workflow notes, user accounts and the audit log. What is encrypted
at rest, and when: [security-measures.md](security-measures.md).

## Support and versions

Versions follow semantic versioning; the canonical schema is versioned
separately, and older schema patch versions stay accepted. Upgrade notes:
[../upgrade.md](../upgrade.md). Support: [support-and-incidents.md](support-and-incidents.md).
