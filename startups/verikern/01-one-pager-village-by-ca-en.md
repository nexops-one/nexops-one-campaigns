# Verikern — one-pager

*Recipient: Emilie Béchet, Le Village by CA Luxembourg. English version of [01-one-pager-village-by-ca.md](01-one-pager-village-by-ca.md). Printable version: [01-one-pager-village-by-ca-en.html](01-one-pager-village-by-ca-en.html).*

---

## Verikern · Sovereign AI gateway for banks and insurers

**Proving, request by request, that a bank's AI complies with the AI Act.**

### The problem

CSSF-supervised banks and insurers already use generative AI, and credit scoring and insurance pricing are **high-risk** uses under the AI Act. Conformity evidence is due by **2 December 2027**. Today:

- AI requests go to US-controlled models, with no proof of where the data went or which model answered;
- compliance evidence is gathered by hand, through questionnaires, after the fact;
- auditors and the CSSF ask for a traceable record that current tools don't produce for the AI Act.

### The solution

A gateway that sits in front of the bank's AI uses:

1. **It routes** every request to an approved model, according to a sovereignty level set for each AI system (EU or self-hosted model by default).
2. **It blocks** what is not allowed: *strictly confidential* data never leaves the bank's infrastructure.
3. **It logs** every request in a signed, tamper-evident record, keeping hashes rather than content.
4. **It generates** the AI Act technical file from that log, instead of writing it by hand.

It runs on the bank's own Kubernetes or OpenShift, with no outbound traffic to Verikern, or as SaaS in a Luxembourg datacentre.

### What sets us apart

- **Compliance as code:** the evidence comes from the running system, not from questionnaires.
- **Verifiable without trusting us:** the log format and the verifier are published; an auditor checks the evidence independently.
- **Sovereign and neutral:** the bank keeps its keys (HSM) and its data, and chooses its models and its cloud.
- **One record, several obligations:** every log field serves the AI Act, DORA's register of ICT providers and the GDPR.

### Customer and business model

- **Buyer:** CIO or head of IT risk; sign-off by the compliance officer.
- **First pilot:** an internal GenAI assistant for staff. Fast to deploy and with no customer-facing risk, it builds evidence the bank can reuse for its high-risk models.
- **Pricing:** annual subscription of €25k to €50k per AI system covered, evidence pack and label included. Range to be validated with the first prospects.

### Where we are (October 2026)

- Architecture and design principles written and settled; technical base chosen (Envoy AI Gateway, Go).
- **Clickable demo**: rules engine, hash chaining, signatures and verifier are real; models and latency are simulated, and labelled as such.
- Customer validation kit ready (interview guide, assessment, letter of intent); first interviews coming up.
- The founder has already built **NEXOPS ONE**, a DORA register tool: the same approach, applied here to AI.
- Full-time on the product from **Q1 2027**.

### Roadmap

| When | Step |
| --- | --- |
| Q4 2026 → Q1 2027 | Interviews, letters of intent, regulatory co-founder, company set-up |
| 2027 | €300k to €500k angel round; MVP; first bank pilot |
| Before 2 Dec 2027 | One paid pilot in production, then a seed round with RegTech investors |

### Team

**Nephthali Djabon**, CEO and technical founder: 20 years in secure, regulated infrastructure. Currently searching for a **regulatory co-founder** (AI Act, CSSF, DORA) and an ML evaluation advisor (SnT or Uni.lu).

### What we are looking for from Le Village by CA

1. **A programme** that fits our stage (pre-seed, product in development).
2. **Conversations with IT Risk or Compliance teams** at the group's Luxembourg entities, to validate the need and prepare a pilot.
3. **Advice** on the 2027 fundraise and on finding a regulatory co-founder.

**Contact:** info@nexopsone.eu · +33619199517 · Demo on request, in person or online.
