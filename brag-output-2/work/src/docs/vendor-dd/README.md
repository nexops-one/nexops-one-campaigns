Status: pending legal review

# Vendor due-diligence pack

DORA (Regulation (EU) 2022/2554, Articles 28 to 30) requires financial
entities to assess their ICT third-party service providers and to record them
in their register of information. This pack answers the usual questions about
the compliance engine and its vendor, NEXOPS ONE, in one place.

**Status.** Every document here is a draft pending legal review. Statements
about the product describe the software as built and documented in this
repository; statements about the vendor's organization, contracts and support
are placeholders until the commercial terms are written. Nothing here is
legal advice or a certification.

| Document | Answers |
|---|---|
| [service-description.md](service-description.md) | what the product does, its deployment modes, where data lives |
| [register/](register/README.md) | the vendor's own register entry, as a canonical batch to import |
| [security-measures.md](security-measures.md) | how data is protected: access control, audit, encryption, network |
| [sbom-and-signatures.md](sbom-and-signatures.md) | how to verify a release: checksums, signatures, SBOM |
| [subprocessors.md](subprocessors.md) | who else processes data (nobody, for the self-hosted modes) |
| [exit-plan.md](exit-plan.md) | how to leave: export formats, no lock-in, what a lapsed license changes |
| [support-and-incidents.md](support-and-incidents.md) | support channels and the incident notification template |

Before the pack is given to a customer: the legal review, the vendor facts of
[register/vendor-facts.json](register/vendor-facts.json), and the placeholders
marked `TO BE COMPLETED`.
