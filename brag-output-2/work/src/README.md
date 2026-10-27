# compliance-engine

A data-source-agnostic compliance engine. It ingests structured data about an
organization's ICT landscape through a documented canonical model, evaluates it
against versioned regulatory control catalogs (DORA first, GDPR and EU AI Act),
and reports readiness with an explicit coverage metric.

Missing data is `not_assessed`, never a pass. Outputs are operational readiness
aids, not legal advice and not certification.

- `sdk/`: adapter SDK (separate Go module, minimal dependencies)
- `pkg/compliance`: embeddable library facade
- `schema/`: canonical schema versions and codelists
- `catalogs/`: control catalogs (data)

Run tests: `go test ./...` (engine), `cd sdk && go test ./...` (SDK) and `cd examples/adapter-go && go test ./...` (example adapter).

Try it: [docs/demo.md](docs/demo.md) (`docker compose -f deploy/demo/compose.yml up`, or `go run ./cmd/compliance-engine demo`). Install: [docs/install.md](docs/install.md) (Kubernetes: the Helm chart in [deploy/helm/compliance-engine](deploy/helm/compliance-engine)); upgrade: [docs/upgrade.md](docs/upgrade.md); backup and restore: [docs/backup-restore.md](docs/backup-restore.md). Web console: [docs/console.md](docs/console.md). Editions: [docs/editions.md](docs/editions.md); writing an edition: [docs/extending.md](docs/extending.md). Generated references (configuration, canonical fields, catalogs, roles): [docs/reference/](docs/reference/). Retention and deletion: [docs/retention.md](docs/retention.md); audit log: [docs/audit.md](docs/audit.md).

Run the server and import spreadsheets: [docs/server.md](docs/server.md). Roles, tokens and the audit log: [docs/access.md](docs/access.md). Evidence and the review workflow: [docs/workflow.md](docs/workflow.md). Reports (Profile B, JSON and PDF): [docs/reports.md](docs/reports.md). Encryption, keys, retention and deletion: [docs/security.md](docs/security.md). Write an adapter: [docs/adapters.md](docs/adapters.md), [examples/adapter-go](examples/adapter-go), and the Python SDK [sdk-python](sdk-python/README.md). Vendor due-diligence pack (DORA third-party assessment; pending legal review): [docs/vendor-dd/](docs/vendor-dd/README.md). Embed the library: [docs/library.md](docs/library.md). Design: [docs/design/](docs/design/).

## License

Apache License 2.0: see [LICENSE](LICENSE) and [NOTICE](NOTICE). Why: [docs/decisions/0001-license.md](docs/decisions/0001-license.md). Contributions are signed off under the Developer Certificate of Origin: [CONTRIBUTING.md](CONTRIBUTING.md). Security reports: [SECURITY.md](SECURITY.md).
