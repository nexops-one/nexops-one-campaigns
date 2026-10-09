# Contributing to compliance-engine

Thank you for helping. This repository is the open core of compliance-engine, licensed under the [Apache License 2.0](LICENSE). The SDK in `sdk/` is a separate Go module under the same license.

## Developer Certificate of Origin

Every commit must be signed off. A sign-off certifies the [Developer Certificate of Origin 1.1](https://developercertificate.org/): you wrote the change, or have the right to submit it under the project's license. There is no contributor license agreement.

Sign off with `git commit -s`, which adds a line with your configured name and email:

```
Signed-off-by: Ada Lovelace <ada@example.com>
```

The line must match the commit author. `scripts/check-dco.sh` checks a range of commits, and CI runs it on every pull request:

```sh
scripts/check-dco.sh origin/main..HEAD
```

The policy applies to every commit after `0a6ddf8` (the last commit made before the policy was adopted). Earlier commits are not rewritten.

To fix a missing sign-off on your last commit: `git commit --amend -s --no-edit`. On several commits: `git rebase --signoff origin/main`.

## Changes

- Run the tests before you send a change: `go test ./...` for the engine, `cd sdk && go test ./...` for the SDK, and `cd examples/adapter-go && go test ./...`. Set `COMPLIANCE_TEST_DATABASE_URL` to run the PostgreSQL tests (see [docs/server.md](docs/server.md)).
- Every Go file starts with `// SPDX-License-Identifier: Apache-2.0`.
- Generated files are checked by tests: after changing configuration, roles, catalogs or the schema, run `go run ./cmd/compliance-engine docs gen`; after changing the demo dataset, run `go run ./demo/gen`.
- New dependencies must use a permissive license (Apache-2.0, BSD, MIT) and need a reason in the pull request.
- Regulatory content (catalogs, codelists, report wording) is indicative and pending legal review. Say so in any change that touches it.

## Reporting security issues

Do not open a public issue. Follow [SECURITY.md](SECURITY.md).
