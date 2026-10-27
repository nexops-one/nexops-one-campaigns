# compliance-engine: brag plan (2:44 demo)

**Requested:** a 2–3 minute demo built from the current implementation, the architecture diagram
(`docs/diagram/compliance-engine.mmd`) and the `demo/` walkthrough. The duration follows the request,
not the 15–25s brag default.

## Answers

- **What is it?** A data-source-agnostic compliance engine. It ingests an organization's ICT landscape through a canonical model, evaluates it against versioned control catalogs (DORA, GDPR, EU AI Act) and reports readiness with an explicit coverage metric.
- **For whom?** Financial entities and the teams preparing DORA registers and readiness: risk/compliance owners, reviewers, approvers and auditors. It is also for the host products that embed it.
- **What sets it apart?** *Missing data is `not_assessed`, never a pass.* A score is always shown next to its coverage. Approvals are tied to an exact evaluation and they expire. Evidence is pinned by sha256. The audit log is hash-chained.
- **Most impressive claim:** "Score 100% … with coverage 47%". The real demo shows this right after import.
- **Visual hook:** that number pair, followed by the `not_assessed` stamp.
- **Real UI shown:** the server-rendered console, captured live from `compliance-engine demo` (built from 313e29b). Screens: sign in → import → dry-run report → commit → completeness → controls → control detail → evidence (sha256) → submit → approve → ready → Profile B PDF → audit log.
- **Tone:** `polished`, with long holds and soft fades. The subject is serious and the product is careful.
- **Share caption:** "Score 100%. Coverage 47%. compliance-engine never lets missing data count as a pass."

## Identity
Dark navy stage (#0b1220) framing the real light console (accent #0b5cad, system fonts, matching console.css).
The diagram scene reuses the .mmd tone colors: blue for ingestion, amber for assessment, mint for review, rose for services, indigo for integration.
All type uses Segoe UI / system-ui, and Consolas for code.

## Storyboard (30 fps, 1920×1080, 164 s)

| # | Time | Scene | On screen |
|---|---|---|---|
| 1 | 0–9 | Hook | "Score 100%" counts up → "with coverage 47%" → "9 of 17 controls had nothing to check." → "Missing data is [not_assessed]. Never a pass." |
| 2 | 9–18 | Reveal | Wordmark; "A data-source-agnostic compliance engine."; chips DORA · GDPR · EU AI Act; ingest → evaluate → review → report |
| 3 | 18–48 | Architecture | The .mmd diagram built group by group (sources → engine and canonical model → catalogs and evaluation → workflow, evidence and reports → one binary: API, console, stores, extensions), with data pulses along the edges |
| 4 | 48–56 | Terminal | `compliance-engine demo`, then its real banner (passwords masked) |
| 5 | 56–154 | Console | Real screens in a browser frame: camera moves, cursor clicks, highlights, a caption under each step (see index.html timeline) |
| 6 | 154–164 | Outro | Wordmark, "Missing data is not_assessed, never a pass.", `docker compose -f deploy/demo/compose.yml up`, repo and Apache-2.0, "readiness aid" disclaimer |

## Sound
An original score generated in code: 100 BPM in A minor (Am–F–C–G). The pad and swell sit alone under the hook, and soft kick, bass and pluck enter at the reveal.
The bed drops back during the outro. SFX are tuned to the key and mixed under the music: soft clicks, whooshes, a low thud for the stamp, and an A/E chime on "ready".
