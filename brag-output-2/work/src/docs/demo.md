# Demo

The demo shows the engine end to end on a **fictitious** organization, "Demo
Bank S.A. (fictitious)": import a spreadsheet, review the validation report
and completeness, find the controls that cannot be assessed, take one control
through evidence, review and approval, and generate a report. Every result is
a demonstration, not a compliance status.

## Start it

With Docker (PostgreSQL and the engine):

```bash
docker compose -f deploy/demo/compose.yml up --build
```

Without Docker (in memory, data lost on exit):

```bash
go run ./cmd/compliance-engine demo          # or: compliance-engine demo --listen 127.0.0.1:8080
```

On first start the engine creates tenant `demo` with workspace `sample`,
flagged as a sample, and four users. Their passwords are printed **once** in
the log:

| User | Roles |
|---|---|
| `owner@demo.invalid` | owner |
| `approver@demo.invalid` | reviewer, approver |
| `auditor@demo.invalid` | auditor |
| `admin@demo.invalid` | admin |

Open <http://localhost:8080/console/>. A later start of the Compose stack keeps
the workspace and prints no new passwords; reset one with
`docker compose -f deploy/demo/compose.yml exec engine /usr/local/bin/compliance-engine user reset-password --tenant demo --email owner@demo.invalid`.
Remove everything with `docker compose -f deploy/demo/compose.yml down -v`.

**Never enable `COMPLIANCE_DEMO` on a deployment holding real data.** The demo
database password is a fixed value and the engine runs without encryption at
rest.

## Walkthrough

1. **Import.** As the owner, open Import and download the sample register
   (also in the repository: `demo/sample-register.xlsx`). Validate it: the
   report shows two rejected rows (an invalid country code, an invalid date),
   one warning (an LEI with a wrong check digit) and missing export-required
   fields. Commit it.
2. **Review the data.** Records shows the canonical model and each field's
   state; Completeness shows the gaps: missing data locations on three service
   lines and three missing exit-plan answers.
3. **Not assessed.** Controls shows DORA with three controls `not assessed`
   (data location, exit plans, and incident readiness, which needs a human
   assessment), and the coverage beside the score.
4. **Evidence and approval.** Open `dora-roi-provider-identification`. As the
   owner, assign yourself and the approver, attach
   `provider-audit-summary.md` as evidence (location
   `file:///demo/evidence/provider-audit-summary.md` with Compose; the `demo`
   command prints its directory; checksum from `demo/evidence/SHA256SUMS`,
   prefixed `sha256:`), verify it and submit. Sign in as the approver and
   approve: the control becomes `ready`.
5. **Report.** On Reports, generate Profile B. The PDF carries "SAMPLE: not a
   compliance status" on every page and the JSON `sample: true`.
6. **Another source.** The same engine consumes a different source: a host
   product (NEXOPS ONE in connected mode) pushes its cloud inventory into a
   second sample workspace. Start the demo with that workspace and a file for
   its token:

   ```bash
   compliance-engine demo --listen 0.0.0.0:8090 --connected-workspace nexops --token-file ./nexops.token
   ```

   (or set `COMPLIANCE_DEMO_CONNECTED_WORKSPACE` and `COMPLIANCE_DEMO_TOKEN_FILE`
   with `COMPLIANCE_DEMO=true`). The workspace is flagged as a sample, the demo
   users get the same roles there, and a service token with the `owner` role is
   written to the file (mode 0600, never printed). Give the host the engine URL
   and that token; an existing token file is kept on restart. In the console,
   switch to workspace `nexops` to see the pushed `cloud_resource` records. In
   the NEXOPS ONE repository, `scripts/compliance-demo-connected.ps1` does all of
   this against its development stack.

## What is in the dataset

`demo/dataset` builds it in code: 12 ICT providers (three in provider
groups), 15 contractual arrangements, 20 service lines, 8 functions, 2
subcontracting chains, signatories, user entities and service assessments.
Every name is invented and every LEI starts with `DEMO00` with valid ISO 17442
check digits (except the deliberate fault). `go run ./demo/gen` regenerates
the workbook and the evidence checksums; a test fails when the committed
files are stale.

## Tests

- `go test ./deploy/demo` runs the walkthrough (steps 1 to 5 through the console sign-in and the API) against the in-memory demo.
- `deploy/demo/demo-smoke.sh` runs it against the Compose stack and checks that a restart does not bootstrap again.
