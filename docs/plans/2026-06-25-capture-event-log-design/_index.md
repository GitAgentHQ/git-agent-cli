# Capture Event Log — Payload-Observed, Tamper-Evident Behavior Capture

Redesign of git-agent's behavior-capture subsystem from diff-reconstruction to a
payload-observed, hash-chained append-only event log serving **audit/provenance**
and **forensic diagnosis**.

- Status: design (pre-merge redesign of the unmerged `feat/code-graph-sqlite` capture subsystem)
- Date: 2026-06-25
- Scope boundary: redesign **only** the Session/Action/CaptureBaseline + `capture`
  command/payload path. The code-graph index (commits/files/AST/co-change/impact)
  is untouched.

## Context

git-agent's capture subsystem records what coding agents do at tool-call
granularity, feeding a `timeline` view and a future `diagnose` command. Today it
does not *observe* what happened — it *reconstructs* it. On each Claude Code
`PostToolUse` hook, `application/capture_service.go:34` (`CaptureService.Capture`)
runs `git diff --name-only`, `git hash-object` on each changed file, and compares
those hashes to a mutable `capture_baseline` table to infer "what this tool call
must have changed." This inference is structurally wrong for the two downstream
uses the maintainer explicitly chose — **audit/provenance** and **forensic
diagnosis** — for three reasons confirmed in the source:

1. **Racy attribution.** Everything in the diff window is blamed on whichever
   capture fires next. Two concurrent agents (distinguished only by
   `instance_id`/`session_id`) are misattributed.
2. **Net-diff loss.** An edit-then-revert nets to zero hash delta and is reported
   `skipped` (`capture_service.go:76-82`), erasing exactly the churn/revert signal
   a forensic investigation follows.
3. **Discarded ground truth.** `cmd/capture_payload.go:11-14`
   (`claudeHookPayload`) parses only `tool_name` and `session_id`, dropping the
   `tool_input` and `tool_response` the hook already delivers — including the Bash
   exit codes that would answer "which action broke the build."

Storage compounds this: `sessions`/`actions`/`action_modifies` plus an
overwritable `capture_baseline` are all **mutable** primary state with no
tamper-evidence — not audit-grade.

**Why now:** the entire subsystem is on an **unmerged branch with zero production
data** (`feat/code-graph-sqlite`). Schema is free to change; there is no migration
burden. This is the moment to make a clean cut.

## Discovery Results

Existing capture pieces (all on `feat/code-graph-sqlite`):

- `application/capture_service.go:34` — `Capture()`: diff-reconstruction.
  `DiffNameOnly` → `HashObject` per file → `GetCaptureBaseline` → delta by hash
  inequality → `DiffForFiles` → `CreateActionBatch`. Returns `skipped` on empty
  delta.
- `domain/graph/session.go` — `SessionNode`, `ActionNode` (stores a 100KB-truncated
  `Diff` string), `CaptureRequest`, `CaptureResult`, `CaptureBaseline`.
- `domain/graph/repository.go:5` — `GraphRepository`; session/action methods plus
  three `CaptureBaseline` methods.
- `infrastructure/graph/sqlite_client.go:16,332` — `CurrentSchemaVersion`,
  `schemaStatements`. Tables `sessions`, `actions`, `action_modifies`,
  `action_produces`, `capture_baseline`. All mutable, no integrity column.
- `cmd/capture.go` — wires flags + payload; on any service error warns to stderr
  and returns nil (correct: a hook must never block the agent).
- `cmd/capture_payload.go:11-14` — the gap: `tool_input`/`tool_response` never
  parsed.
- Existing seams to **reuse**: `application/action_linker.go:30`
  (`GraphActionLinker.LinkActionsToCommit`) + `action_produces` for event↔commit
  linking; `ResolveRenames` for rename-aware history; `ImpactService.Impact` /
  `ImpactEntry.CouplingStrength` for blast-radius; `Timeline`.

**The gap:** the hook delivers an authoritative record of one tool call; the system
throws it away and re-derives a lossy, racy, net-only approximation in mutable
tables with no `verify`, no `rebuild`, no exit-code capture.

## Glossary

Canonical domain nouns, reconciled across research streams. Rejected variants are
recorded so future readers see what was considered.

| Concept | Canonical label | Rejected variants |
|---|---|---|
| Source-of-truth record of one captured fact | **Event** (`EventRecord`, table `events`) | `ActionNode` (now a projection) |
| The append-only hash-linked log (one per repo) | **Event Log** / **Chain** | "behavior log" |
| Per-event integrity links | **`prev_hash`** / **`this_hash`** | `row_hash` |
| Monotonic global ordering column | **`seq`** | `sequence_number`, `sequence` (the latter is the per-session projection field) |
| First event's predecessor link | **Genesis** (`prev_hash` = 64 hex zeros) | "0", "" |
| Deterministic bytes fed to the hasher | **Canonical Form** | "JCS form" (full RFC 8785 rejected as unneeded) |
| Domain port for canonicalize-and-hash | **`EventHasher`** (impl `SHA256Hasher`) | — |
| Integrity-check operation | **`graph verify`** → **`VerifyResult`** | `EventChainReport`, `VerifyService` (the op lives in a service but the result type is `VerifyResult`) |
| A detected tampering | **`ChainBreak`** (`kind`: `ROW_EDITED` / `ROW_DELETED` / `ROW_INSERTED` / `ROW_REORDERED`) | "first_invalid_sequence" (a field, not the concept) |
| Test/build result record | **Outcome Event** (`kind = "outcome"`) | — |
| A change with no captured hook | **Out-of-Band Event** (`source = "unknown"`, `kind = "out-of-band"`) | "External-Edit Event", "Reconciled Event" |
| The gap an Out-of-Band Event closes | **Blind Spot** | — |
| The diff-based fallback process | **Reconciliation** (`ReconcileService`) | — |
| Derived, rebuildable views | **Projection** (sessions, actions, action_modifies, action_produces) | "read model" (synonym, acceptable in prose) |
| Regenerating projections from the log | **Rebuild** (`graph rebuild`) via **Replay** (`ProjectionRebuilder`) | — |
| Cold-path computation of derived data | **Enrichment** | "background pass" |
| Per-touched-file derived row | **`event_files`** table | — |
| git OID of a file version | **File Blob Ref** (`before_blob` / `after_blob`) | bare "blob reference" |
| Hash-only storage replacing secret content | **Redaction Digest** (`sha256` + length) | bare "blob reference" |
| Secret-masking outcome | **Redaction** → **Typed Placeholder** `[REDACTED:<rule-id>]` | — |
| Verbatim stored stdin bytes (post-redaction) | **`payload_raw`** | — |
| Hot vs cold capture stages | **Append** (hot path) vs **Enrichment** (cold path) | — |
| Forensic command | **`graph diagnose`** → **`DiagnosisResult`** | — |
| last-green → first-red span | **Suspect Window** | "bisect interval" |
| A ranked suspect / the ranked set | **Candidate** / **Candidate Ranking** | — |
| File change history view | **`graph provenance`** → **Provenance View** | — |

## Requirements

YAGNI applied. Each requirement maps to **[A]udit/provenance** or **[F]orensics**;
infrastructure requirements are **[I]**.

### Functional

| # | Requirement | Maps |
|---|---|---|
| FR1 | **Observe the payload verbatim.** Parse and persist `tool_name`, `tool_input`, `tool_response`, `session_id`, `cwd`, `transcript_path`, `hook_event_name`, `permission_mode` as the Event's recorded fact. Do not re-derive what the payload already states. | A |
| FR2 | **Append-only Event Log as single source of truth.** One immutable `events` row per captured hook call. No `UPDATE`/`DELETE`/`INSERT OR REPLACE` on `events`. | A |
| FR3 | **Hash-chain the log.** Each Event stores `prev_hash` and `this_hash` over the Canonical Form. Genesis `prev_hash` = 64 hex zeros. | A |
| FR4 | **`graph verify`** — walk the chain, classify the first `ChainBreak` (`ROW_EDITED`/`ROW_DELETED`/`ROW_INSERTED`/`ROW_REORDERED`), exit code 4 on break. | A |
| FR5 | **Outcome Events.** From Bash `tool_response`, record `exit_code` (+ `exit_code_source` = reported/inferred), `is_test`/`is_build`, `test_name`, so "which action broke it" is answerable. | F |
| FR6 | **Projections, not primary state.** `sessions`/`actions`/`action_modifies`/`action_produces` become read models projected from the Event Log. | A,F |
| FR7 | **`graph rebuild`** — drop and Replay all projections from the Event Log; deterministic and byte-identical across runs. | A,F |
| FR8 | **Preserve every Event including reverts.** Capture is unconditional per hook call; edit-then-revert is two Events, never a net `skipped`. | F |
| FR9 | **Out-of-Band reconciliation net.** A diff-based fallback emits `source=unknown` Events for changes no hook reported, so human/external edits are not Blind Spots. | A,F |
| FR10 | **Correct concurrent attribution.** Attribution comes from the payload `session_id`/instance identity, not a time window. | A,F |
| FR11 | **Bounded payload storage.** Truncate oversized payload fields at a fixed cap (or store a Redaction Digest); record original size and a truncation flag. The hash covers the stored bytes. | A |
| FR12 | **Secret Redaction.** Sensitive file paths → Redaction Digest; common secret token formats → Typed Placeholder, before storage. | A |
| FR13 | **Hook must never block.** Any capture failure exits 0 with a stderr warning (preserve `cmd/capture.go` behavior). | I |
| FR14 | **`graph provenance <file>`** — chronological, rename-aware change history of a file from the Event Log + Out-of-Band Events. | A |
| FR15 | **`graph diagnose "<symptom>"`** — deterministic Candidate Ranking over the Suspect Window, optional bounded LLM re-rank of top-N. Runs `verify` first; refuses on a broken chain unless `--force`. | F |

### Non-Functional

| # | Requirement | Maps |
|---|---|---|
| NFR1 | **Tamper-evidence, not tamper-proofing.** Plain SHA-256 chain; no signing/keys in v1. Framed honestly: detects post-hoc edits; cannot prevent a user who owns `graph.db` from recomputing the chain. | A |
| NFR2 | **Clean Architecture preserved.** Event value objects/ports in `domain/graph`; orchestration in `application/`; SQLite + hashing in `infrastructure/`; `cmd/` wiring only. | I |
| NFR3 | **Append latency.** Hot path is a single small INSERT; no full-tree hashing, diff, AST, or projection work on the hook path. Stays under the current ~45ms budget. | I |
| NFR4 | **Offline, pure-Go SQLite** (`modernc.org/sqlite`), WAL + `synchronous=NORMAL`, single writer, silent-skip on lock contention. | I |
| NFR5 | **Deterministic Canonical Form.** Fixed field order + payload-bytes hash; versioned; v1 frozen forever. | A |
| NFR6 | **Pre-merge clean cut.** Delete `capture_baseline` as primary state and the hash-reconstruction path; no migration shim. | I |

### Out of scope (flagged gold-plating)

- Cryptographic signing / HMAC-keyed chain / Merkle trees / external head-hash
  anchoring — over-engineering for a local single-user tool; noted as opt-in
  future hardening in `best-practices.md`.
- ML / preference learning ("what the agent likes") — maintainer explicitly
  rejected this direction.
- LLM timeline compression, MCP mode — already P2, unaffected.
- Touching the code-graph index (commits/files/AST/co-change/impact) — hard scope
  boundary.
- Encrypting the log — integrity is the goal, not confidentiality.
- Full gitleaks-library + entropy detector — v1 ships a small compiled regex set +
  path denylist; deeper detection is future hardening.

## Rationale

**Why event-sourcing + hash chain + payload observation, for these two goals
specifically.**

- **Audit/provenance demands an immutable, ordered record of what was asserted, by
  whom.** The current mutable graph + overwritable `capture_baseline` has no answer
  to "was this history edited after the fact?" An append-only, hash-chained log
  makes any post-hoc edit detectable (`graph verify`) without keys or signing — the
  minimum mechanism that satisfies tamper-*evidence*. Making
  sessions/timeline **derived** means there is exactly one source of truth to
  protect; a corrupted Projection is recoverable via Rebuild, and a corrupted Event
  Log is detectable via verify.
- **Forensic diagnosis demands the full, lossless sequence including high-signal
  events.** Reverts and churn are exactly what a "which action broke it"
  investigation follows; net-diffing erases them. Observing the payload keeps the
  actual action, and capturing Bash exit codes (already in `tool_response`,
  currently discarded) turns "which action broke the build" from unanswerable into a
  single query. Time-window reconstruction can never attribute concurrent agents
  correctly; the payload's own `session_id` can.

**Why observe instead of reconstruct:** the hook hands over ground truth.
Diff-reconstruction is strictly lossier (net-only, racy) and *more* expensive
(full-tree `hash-object`). Its only legitimate residual role is the Reconciliation
net for Blind Spots — and even there it is labeled `source=unknown`, never
masquerading as observed fact.

**Why the scope boundary:** the code-graph index derives from git history, which is
itself an append-only hash-chained log (git). It is validated and serves a
different consumer (structural `impact` queries). Capture is the only part that is
unmerged, records agent behavior, and has the audit/forensic failure modes.

## Detailed Design

The Event Log is one SQLite `events` table per repo (`.git-agent/graph.db`),
append-only and hash-chained. The hot **Append** path does one tiny INSERT of the
verbatim, redacted payload. A cold **Enrichment** path replays Events into
Projections (`sessions`/`actions`/`action_modifies`/`action_produces`) and
materializes `event_files` (File Blob Refs + churn) for diff reconstruction.
**Reconciliation** appends `source=unknown` Out-of-Band Events for working-tree
changes no hook explained. **`graph verify`** walks the chain and classifies the
first `ChainBreak`. **`graph provenance`** and **`graph diagnose`** read Events +
Projections; diagnose runs verify first.

Full specifications:

- Schema, layering, hash-chain construction, projection rebuild, reconciliation,
  and integration points: `architecture.md`
- Gherkin scenarios (happy path, tamper-evidence, reconciliation, outcome events,
  redaction, non-blocking, rebuild, concurrency, edge cases): `bdd-specs.md`
- Security (redaction, tamper-evidence altitude, canonicalization), performance
  (sub-ms append, WAL, deferred enrichment), pitfalls: `best-practices.md`

## Design Documents

- [architecture.md](architecture.md) — system overview, `events`/`event_files`
  schema, clean-arch placement, hash chain, projections, reconciliation, verify &
  diagnose surfaces, integration points.
- [bdd-specs.md](bdd-specs.md) — full Gherkin scenarios.
- [best-practices.md](best-practices.md) — security, performance, testing, common
  pitfalls.
