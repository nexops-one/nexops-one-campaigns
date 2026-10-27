# Evidence, review workflow and effective status

The engine computes a status for every control from register data (see
[server.md](server.md)): `not_assessed` when inputs are missing, `in_review`
with attention `rule_failed` when the rule is not satisfied, and `monitoring`
when it is. **Only people make a control `ready`**: an owner submits it with
evidence, and an approver who is not the owner approves it. This page describes
that workflow. Roles and permissions are in [access.md](access.md).

## Evidence

Evidence is a **reference** to an object that stays in your environment (a
document in a DMS, a GRC record, a file share), with its SHA-256 checksum. The
engine never stores the object.

```bash
curl -X POST https://engine/api/v1/evidence -H "Authorization: Bearer $TOKEN" -d '{
  "title": "Exit plan for Cloud Provider X", "kind": "document", "source": "SharePoint",
  "uri": "https://dms.example.com/sites/risk/exit-plan-x.pdf",
  "checksum": "sha256:<64 hex>", "valid_until": "2027-06-30T00:00:00Z",
  "retention": {"min_days": 3650, "basis": "DORA record keeping"},
  "links": [{"catalog": "dora", "control_id": "dora-exit-plans"}]}'
```

| Field | Rules |
|---|---|
| `kind` | `document`, `attestation`, `report`, `screenshot`, `log_extract`, `other` |
| `uri` | `https`, `s3`, `file` or `urn`. URIs carrying credentials are refused: user information, and query parameters named like `token`, `key`, `secret`, `sig`, `password`, `credential` or `X-Amz-*`. Reference the object, not a signed download link. |
| `checksum` | `sha256:<64 lowercase hex>`, computed by whoever collects the evidence. Required, unless the deployment sets `COMPLIANCE_EVIDENCE_ALLOW_NO_CHECKSUM=true` (see below) |
| `links` | controls the evidence supports; one item can support several controls (`POST /api/v1/evidence/{id}/links`, `DELETE /api/v1/evidence/{id}/links/{catalog}/{control}`) |

**State.** An evidence item is `revoked` (`POST .../revoke` with a reason),
else `integrity_failed` (its latest check did not match), else `expired`
(after `valid_until`), else `active`. Revoked and expired evidence stays
readable for audit.

**Integrity.** `unverified` when attached, then `verified` or `failed` after
each check. Every check is kept with its method, actor and observed checksum.
A failed check downgrades every approved control the item supports, and a
later matching check restores it.

**Evidence without a checksum.** Some systems never recorded checksums. With
`COMPLIANCE_EVIDENCE_ALLOW_NO_CHECKSUM=true` (off by default; the NEXOPS ONE host
turns it on for evidence carried over from its earlier module), evidence may be
attached without one. It has integrity `no_checksum`: it stays `active` and
readable, but it **never satisfies the evidence rule**, and reports say the
checksum is missing. The engine cannot check it (`409 no_checksum`). The first
attested check (`compliance-engine evidence verify --file`) records the checksum
the customer computed (check method `attested_adopt`, integrity `verified`), and
later checks compare against it.

| How | When |
|---|---|
| `POST /api/v1/evidence/{id}/verify` | the engine hashes the object itself: `file://` paths under `COMPLIANCE_EVIDENCE_ROOT` (symlinks resolved, no escape), or `https://` hosts listed in `COMPLIANCE_EVIDENCE_FETCH_ALLOW` (redirects only to listed hosts). Anything else answers `422 cannot_verify_here`. |
| `compliance-engine evidence verify --url U --token T --file PATH <id>` | the customer hashes a local copy; only the checksum is sent (`POST .../checks`). Exit code 1 on a mismatch. |

By default the engine reads no evidence and makes no outbound calls.

**Access logging.** Reading an evidence item or a list of evidence (both return
URIs) appends an `evidence.access` event with the IDs to the audit log.
Evidence creation is logged with the location's scheme and host, not the full URI.

## Assessments and actions

Each control has an assessment per workspace, keyed by catalog name and control
ID so it survives catalog updates. It holds an owner, a reviewer, a due date,
notes, a **stage** (`none`, `submitted`, `rejected`, `approved`), the current
approval, and an append-only **history** of transitions (actor, time, previous
and new stage and status, reason, evaluation).

| Action | Route (`/api/v1/assessments/{catalog}/{control}`) | Permission | Allowed when |
|---|---|---|---|
| assign | `PUT` `{owner, reviewer, due_at, notes}` (emails; `""` clears) | `workflow.assign` | always; the owner must hold `owner`, the reviewer `reviewer` or `approver` |
| submit | `POST .../submit` | `workflow.submit` | stage `none` or `rejected`, or an expired approval (re-review); the control is assessable; by the owner (the first submitter becomes owner when none is assigned) |
| recommend | `POST .../recommend` `{note}` | `workflow.review` | stage `submitted` |
| reject | `POST .../reject` `{reason}` | `workflow.review` | stage `submitted`; reason required |
| approve | `POST .../approve` `{evaluation_id}` | `workflow.approve` | stage `submitted`; not the owner (unless `COMPLIANCE_ALLOW_SELF_APPROVAL=true`, then flagged); evidence rule below; the evaluation is current |
| reopen | `POST .../reopen` `{reason}` | `workflow.submit` | stage `approved`; by the owner; reason required |

A control is **assessable** when its computed status is `monitoring`, or when it
is a `manual` control (its rule needs a human assessment and the engine reports
`not_assessed` with the single blocker `requires_human_assessment`). A control
whose data is incomplete or whose rule fails cannot be submitted.

**Evidence rule.** Approval needs at least one linked `active` evidence item
with a checksum when the control lists `evidence_requirements`, is `manual`, or
sets `approval_requires_evidence: true` in its catalog. A linked item whose
integrity check failed always blocks approval (`409 evidence_required`).

**Current evaluation.** The approver names the evaluation they reviewed. The
engine re-evaluates the current snapshot; if the evaluation is of another
snapshot, or the control's computed status changed, approval is refused with
`409 stale_evaluation`.

**Workflow policy.** What an approval needs is decided per control by the
deployment's workflow policy (`extension.WorkflowPolicy`). The open core's
policy is the single-approver workflow described above. An edition's policy
(the commercial advanced workflow) may require:

- **several distinct approvers** (a quorum of up to 5): each `approve` below the
  quorum is recorded in the assessment's `approvals` and the history (`approvals`
  and `required_approvals` on the transition); the stage stays `submitted`, the
  effective status `in_review` with attention `awaiting_approval`, and status
  payloads show `approvals: {given, required}`. At the quorum the stage becomes
  `approved` and the approval lists every approver (`approvers`). The same person
  approving twice is refused (`409 already_approved`); each approval is checked
  for a stale evaluation;
- **a reviewer's recommendation first** (`409 recommendation_required`);
- **an approver other than the submitter** (`409 submitter_approval_forbidden`).

A new `submit`, a `reject` or a `reopen` clears the recommendation and the
approvals given so far.

**Review interval.** An approval holds for the control's `review_interval`
(ISO 8601 period in the catalog, for example `P1Y`; default `scoring.review_interval`,
then `P365D`). Afterwards the control is `expired` until it is resubmitted and approved again.

Every action appends a transition and an `assessment.<action>` audit event in
the same transaction.

**Imported assessments.** A host product migrating its own earlier assessments
calls `Engine.ImportAssessment` (Go library only, not the HTTP API) as an
operator. It sets owner, notes, due date and, optionally, an approval given in
the other system, with one `import` transition and an `assessment.import` audit
event. It never overwrites an assessment the engine already has. An imported
approval is marked `imported`, has no evaluation, and follows the effective-status
rules below like any other: it is voided when the control is not assessable or
its evidence does not hold, and expires at the date it carried over.

## Effective status

Every evaluation (`POST /api/v1/evaluations`) returns the engine's computed
`result` and an `effective` result combining it with assessments and evidence
at the evaluation time. `GET /api/v1/status?catalogs=dora@1.0.0` returns the
same effective view live, without storing anything.

| Computed | Stage | Effective status | Attention |
|---|---|---|---|
| `not_assessed` (inputs missing) | any | `not_assessed` | |
| `in_review` (rule failed) | any | `in_review` | `rule_failed` |
| assessable | `none` | computed status (`monitoring`; `not_assessed` for a manual control) | |
| assessable | `submitted` | `in_review` | `awaiting_approval` |
| assessable | `rejected` | `rejected` | |
| assessable | `approved`, evidence missing, revoked, expired, failed, or only without a checksum | `in_review` | `evidence_invalid` |
| assessable | `approved`, past the review interval | `expired` | `approval_expired` |
| assessable | `approved` | **`ready`** | |

An approval that no longer holds (inputs incomplete, rule failed, evidence
invalid) is reported with a `void_reason`. The next stored evaluation records
the void as a `system` transition and `assessment.void` audit event, and the
control returns to stage `none`.

`overdue` is set when the due date has passed and the control is not `ready`;
it never changes the status.

**Score and coverage** use effective statuses with each catalog's scoring
rule: `score = (ready + monitoring) / assessable`, `coverage = assessable / in scope`.
`rejected` and `expired` are assessable and never count as ready. The computed
`result` never contains `ready`.

**Reproducibility.** A stored effective result embeds its inputs (the
assessments and the state-relevant fields of the evidence used, never their
URIs) and its `as_of` time. Recomputing it from the stored computed result and
those inputs gives the same result, whatever happened in the workflow since.

## Workspace overrides

Admins can replace a control's catalog review settings in one workspace with
`PUT /api/v1/settings`:

```json
{"review_overrides": {"dora/dora-exit-plans": {"review_interval": "P6M", "approval_requires_evidence": true}}}
```

Overrides apply to new approvals (the expiry is fixed when a control is
approved) and to the evidence rule in effective results. Effective results
record the overrides they used in `inputs.overrides`, so they stay reproducible.
