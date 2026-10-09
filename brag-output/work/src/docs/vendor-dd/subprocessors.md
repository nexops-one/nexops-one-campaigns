Status: pending legal review

# Subprocessors

## Self-hosted, offline, embedded and connected modes

**None.** In these modes the customer (or the operator of its NEXOPS ONE
deployment) runs the software in its own environment. The vendor does not
host, store, process or have access to customer data, and the software sends
nothing to the vendor or to any third party: no telemetry, no update or
license check over the network.

Outbound connections the customer may enable, to its own choice of parties:

| Setting | Connects to |
|---|---|
| `COMPLIANCE_EVIDENCE_FETCH_ALLOW` | the evidence hosts listed, to verify checksums |
| `COMPLIANCE_OIDC_ISSUER` (enterprise) | the customer's identity provider |
| a connected NEXOPS ONE workspace | the customer's own engine |

These are the customer's providers, not the vendor's subprocessors.

## Hosted offering

Not offered. If a hosted tier is introduced, this page will list its
subprocessors (hosting, storage, backup, support tooling), the data each
processes, their locations and the notification process for changes
(spec v2 §12 item 7).

## Vendor support

Support does not require access to customer data. When a customer chooses to
send logs or exports to support, it decides their content; logs contain no
secret values and the audit log no free text. TO BE COMPLETED: the vendor's
support tooling, if it is operated by a third party.
