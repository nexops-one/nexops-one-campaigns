# Web console

`serve` serves a web console at `/console/` (turn it off with
`COMPLIANCE_CONSOLE=false`). It is server-rendered HTML with one embedded
stylesheet: no JavaScript, nothing loaded from outside the binary. Every page
and action requires the same permission and feature as the API route doing the
same operation ([reference/roles.md](reference/roles.md)); buttons you may not
use are not shown.

## Signing in

Users sign in with their email and password, created by an operator
(`compliance-engine user create`, `user reset-password`). When
`COMPLIANCE_CONSOLE_TENANT` is set the form does not ask for the tenant. A
member of several workspaces chooses one after signing in and can switch later.

- The session cookie is `HttpOnly`, `Secure` and `SameSite=Strict`, scoped to `/console`. Only its SHA-256 is stored.
- Sessions end after `COMPLIANCE_SESSION_IDLE` without a request (default 30 minutes) and `COMPLIANCE_SESSION_MAX` after sign-in (default 12 hours). Removing a member or a role takes effect on the next request.
- Every form carries a CSRF token derived from the session. Responses carry a strict `Content-Security-Policy` (`default-src 'self'`), forbid framing and are not cached.
- Failed sign-ins share the per-address failure budget of the API (`COMPLIANCE_AUTH_FAILURE_LIMIT`). Sign-ins and sign-outs of known users are audited ([audit.md](audit.md)).
- Because the cookie is `Secure`, browsers send it over HTTPS and to `localhost` only.

## Pages

| Page | What you can do |
|---|---|
| Controls | per framework, the score always beside its coverage; each control's effective status, attention, review stage, owner, due date |
| Control detail | rule, inputs, failing records and blockers; linked evidence (attach by reference, link, verify the checksum, revoke); assign, submit, recommend, reject, approve, reopen; history |
| Import | download the workbook or CSV templates; validate a file (dry run: row and field errors, warnings, missing export-required fields), then commit it; roll back an ingestion |
| Records | entities, records with their field states, provenance |
| Completeness | gaps per entity, split into supplied and `not_supplied_by_any_adapter`; invalid codes; unverified codelists; dangling references and cycles |
| Reports | profiles and their entitlement; generate; download the files (audited); verify by regeneration |
| Settings | your API tokens (the value is shown once); members and roles, retention and review overrides (admins) |
| Audit | the workspace's audit chain and its verification (auditors and admins) |

Approving a control on the console stores an evaluation of what the page
showed and approves on that basis; if the data changes before you click,
the approval is refused (`stale_evaluation`) and the page shows the new state.

A validated import is kept in the server's memory for 30 minutes for the
commit. With several replicas, use sticky sessions, or upload the file again
when the commit says it expired.

## Sample workspaces

In a workspace flagged as a sample (the demo), every page shows "Sample data:
fictitious organization. Results are a demonstration, not a compliance
status." The flag cannot be turned off from the console. Reports generated
there are watermarked ([reports.md](reports.md)).
