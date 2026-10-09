# 0001: Apache License 2.0 and the Developer Certificate of Origin

- **Status:** accepted (2026-09-30)
- **Context:** compliance-service specification v2, SPEC-LIC-001, required choosing between Apache 2.0 and the AGPL for the open core, and recording the reason before the first public release.

## Decision

1. The open repository (`compliance-engine`) and the SDK module (`sdk`) are licensed under the **Apache License 2.0**.
2. Contributions are accepted under the **Developer Certificate of Origin** (`Signed-off-by`), enforced in CI. There is no contributor license agreement.
3. The commercial edition lives in a separate, private repository (`compliance-engine-enterprise`) under proprietary terms, and plugs into the open core through its public extension points (`extension.ReportProfile`, `extension.Entitlements`, `extension.Authenticator`, `extension.WorkflowPolicy`).

## Reasons

- **Adoption by regulated entities.** Banks, insurers and their integrators review licenses before they deploy. Apache 2.0 is on every approved list; the AGPL's network clause is often refused outright, or needs legal review for each deployment.
- **Adapters and integrations.** Customers and partners write adapters against the SDK and embed the library. Apache 2.0 lets them do so without having to publish their own code.
- **Explicit patent grant.** Apache 2.0 grants patent rights from contributors and ends them for anyone who sues over patents in the work. MIT and BSD have no such grant.
- **The boundary is kept by the repository, not by copyleft.** Commercial features are not in the open repository at all, so copyleft is not needed to protect them. The commercial edition adds a test asserting that the open-core binary contains no enterprise package (Plan 11).
- **No CLA.** The DCO is lighter for contributors and is enough under Apache 2.0: the project never needs to relicense contributions, because the open core stays Apache 2.0.

## Consequences

- Every Go file carries `// SPDX-License-Identifier: Apache-2.0` (checked by a test). `NOTICE` lists bundled third-party material (the DejaVu fonts).
- Dependencies must be permissively licensed.
- Anyone may build a competing service on the open core. This is accepted: the commercial value is in the maintained catalogs, Profile A, SSO, advanced workflow and support.
- The license terms of the commercial edition are still a placeholder (cycle 2 design §16).
