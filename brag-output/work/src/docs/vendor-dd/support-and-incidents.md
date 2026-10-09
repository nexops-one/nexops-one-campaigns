Status: pending legal review

# Support and incident notification

## Support

| Item | Commitment |
|---|---|
| Channels | TO BE COMPLETED (support address, portal) |
| Hours and response times | TO BE COMPLETED, per support level of the commercial terms |
| Security vulnerabilities | reported as described in [../../SECURITY.md](../../SECURITY.md) (contact TO BE COMPLETED) |
| Versions supported | TO BE COMPLETED (for example: the latest minor version and the one before) |
| Open core | community support through the public repository; no commitment |

Support works without access to customer data: the customer runs the software
and decides what it shares (logs hold no secret values; the audit log holds no
free text).

## Incidents the vendor notifies

The vendor does not operate the software for its customers in the modes
offered today, so the incidents it notifies are about the software itself:

- a vulnerability affecting a supported version, with its severity, the affected versions, the fix or workaround and the release that contains it;
- a defect that may have produced wrong results (for example a wrong evaluation, a wrong report figure), with what to re-run;
- a compromise of the vendor's release signing key or build pipeline, with the releases concerned.

The customer remains responsible for classifying and reporting its own major
ICT-related incidents to its authority (DORA Articles 17 to 19); the
notification below gives it what it needs to do so.

## Notification template

```
Subject: [compliance engine] Incident notification <reference> - <severity>

Reference:            <vendor reference>
Sent:                 <date and time, UTC>      Sender: <name, role, contact>
Status:               initial | update <n> | final

What happened:        <description, in plain words>
Detected:             <date and time, UTC>      Started (if known): <date and time, UTC>
Products and versions affected: <open core / enterprise, versions, deployment modes>
Severity:             <critical | high | medium | low>, because <reason>

Impact on your data and results:
  - confidentiality:  <none / description>
  - integrity:        <none / which results may be wrong, since when>
  - availability:     <none / description>

What you should do now: <steps: upgrade to, configuration change, re-run evaluations or reports>
What we are doing:      <actions, next update expected at>
Fix:                    <release and checksum, or workaround>

Contact for questions:  <channel>
```

TO BE COMPLETED: the notification deadlines (initial, update, final) and the
channel, set in the commercial terms.
