# Editions

compliance-engine has an open core (this repository, Apache-2.0) and a
commercial edition that adds features through the open core's extension
points. Reads never depend on a license: when a license expires, stored data,
evaluations and reports stay readable, exportable and verifiable.

| Capability | Open core | Commercial |
|---|---|---|
| Canonical model, SDK (Go), adapters, ingestion, spreadsheet import | ✓ | ✓ |
| Catalogs shipped with the engine (DORA, GDPR, EU AI Act; indicative) | ✓ | ✓ |
| Maintained catalogs and signed offline catalog feed (`catalog.maintained`) | | ✓ |
| Evaluation, effective status, completeness | ✓ | ✓ |
| Evidence lifecycle, single-approver review workflow | ✓ | ✓ |
| Advanced workflow: quorum approvals, required recommendations, approver other than the submitter, bulk actions (`workflow.advanced`) | | ✓ |
| Profile B readiness and evidence report (JSON, PDF) | ✓ | ✓ |
| Profile A register of information export, xBRL-CSV, pre-submission validation (`report.profile_a`; indicative) | | ✓ |
| Local accounts, roles, API tokens, audit log, encryption at rest, retention | ✓ | ✓ |
| Web console | ✓ | ✓ |
| Workspace registry, suspension, workspace deletion (library) | ✓ | ✓ |
| SSO (OIDC) with claim-to-role mapping (`auth.sso`) | | ✓ |
| Multi-tenant administration API and usage against the license (`tenancy.multi`) | | ✓ |

## How the boundary works

- The commercial edition is a separate binary, `compliance-engine-enterprise`,
  built from a private repository. It runs `cli.Main` with its own
  `cli.Edition`, so it reuses every open command and the open server unchanged,
  and adds its features as plugins (see [extending.md](extending.md)):
  - `extension.Entitlements` (the license), with `extension.WorkspaceLimiter`;
  - `extension.ReportProfile` and `extension.ParameterizedProfile` (Profile A);
  - `extension.Authenticator` and console sign-in providers (SSO);
  - `extension.WorkflowPolicy` (the advanced workflow) and workspace settings extensions;
  - a `catalog.Source` whose catalogs carry a feature (the catalog feed);
  - extra API routes, handlers and `GET /api/v1/about` sections.
- Features are checked by name through `extension.Entitlements`. The open core
  allows its own features and nothing else.
- A license file is verified offline (Ed25519). After expiry there is a grace
  period during which commercial features work and API responses carry
  `Compliance-License-Warning` (and `Warning: 299`); after that, commercial
  features are refused with `403 feature_not_entitled`. Nothing is deleted or
  hidden, and a missing or invalid license never stops the server.
- Without the license, a maintained catalog version is skipped when a
  workspace evaluates the latest catalogs; the advanced workflow falls back to
  the single-approver workflow (approvals under way complete at the next
  approval); suspending, reading and deleting workspaces stay possible.
- The open core builds and runs alone: it contains no commercial package
  (`TestOpenCoreHasNoEnterpriseDeps`).

Why Apache-2.0: [decisions/0001-license.md](decisions/0001-license.md).
