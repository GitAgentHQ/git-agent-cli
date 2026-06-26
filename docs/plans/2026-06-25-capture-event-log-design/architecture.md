# Architecture — Capture Event Log

All paths relative to the repo root; line refs are into `feat/code-graph-sqlite`
(read via `git show feat/code-graph-sqlite:<path>`).

## System Overview

```
Claude Code PostToolUse
        │ stdin: full payload JSON
        ▼
  cmd/capture.go  ──parse──▶  cmd/capture_payload.go (full claudeHookPayload)
        │                           │ redact (paths → Redaction Digest, tokens → placeholder)
        │                           ▼
        │                     domain.EventRecord (payload_raw = redacted bytes)
        ▼
  application.CaptureService.Capture     ── HOT PATH (Append) ──
        │  1. repo.HeadHash()          (prev_hash)
        │  2. hasher.Hash(canonical)   (this_hash)
        │  3. repo.AppendEvent()       (one BEGIN IMMEDIATE INSERT)
        ▼
   .git-agent/graph.db :: events  (append-only, hash-chained)
        │
        │  ── COLD PATH (Enrichment / Replay) ──  triggered by graph rebuild / EnsureIndex / commit
        ▼
  application.ProjectionRebuilder ─▶ sessions, actions, action_modifies, action_produces, event_files
  application.ReconcileService    ─▶ appends source=unknown Out-of-Band Events for Blind Spots
        ▼
   Read surfaces: graph verify | graph provenance <file> | graph diagnose "<symptom>" | timeline
```

Two invariants drive the whole design:

1. **The Event Log is the only source of truth.** Everything else (sessions,
   actions, co-change-of-actions, timeline, `event_files`) is a Projection that
   `graph rebuild` can regenerate by Replay.
2. **The hot path appends only.** All git/diff/AST/projection work moves to the
   cold Enrichment path. The payload already states what changed, so no `git diff`
   or `git hash-object` runs on the hook.

## Clean-Architecture Placement

Mirrors the existing `session.go` / `repository.go` / `sqlite_*` split.

### domain/ (zero external imports)

New file `domain/graph/event.go`:

```go
type EventKind string // "tool" | "outcome" | "out-of-band"

type EventSource string // "claude-code" | "cursor" | "human" | "unknown"

// EventRecord is the immutable, hash-chained source-of-truth record.
type EventRecord struct {
    Seq            int64       // monotonic global ordering (assigned at append)
    EventID        string      // ULID/UUID
    RecordedAt     int64       // unix seconds, capture wall-clock
    Source         EventSource
    InstanceID     string      // = payload session_id; distinguishes concurrent agents
    Kind           EventKind
    HookEventName  string      // "PostToolUse"
    ToolName       string
    Cwd            string
    TranscriptPath string
    PermissionMode string
    PayloadRaw     []byte      // verbatim, post-redaction stdin bytes (hashed unit)
    PayloadSize    int64       // original size before truncation
    Truncated      bool
    // Outcome fields (zero unless Kind == "outcome")
    Command        string
    ExitCode       *int
    ExitCodeSource string      // "reported" | "inferred"
    IsTest         bool
    IsBuild        bool
    TestName       string
    // Chain
    PrevHash       string
    ThisHash       string
}

// EventHasher canonicalizes-and-hashes. Hashing is infra (SHA-256), so domain
// depends on this port, exactly as it already does with SessionIDGenerator.
type EventHasher interface {
    Hash(prevHash string, e EventRecord) string
}
```

`CaptureBaseline` (`session.go:51-56`) is **deleted**. `SessionNode` and
`ActionNode` remain but are demoted to Projection DTOs.

### application/

- `CaptureService.Capture` (`capture_service.go:34`) is gutted of diff/baseline
  logic. New flow: build `EventRecord` from the request → `repo.HeadHash()` →
  `hasher.Hash()` → `repo.AppendEvent()`. No `DiffNameOnly`/`HashObject`/
  `GetCaptureBaseline` on the hot path. Gains a `hasher graph.EventHasher` field.
- New `application/projection_service.go` — `ProjectionRebuilder` (the Replay
  engine).
- New `application/reconcile_service.go` — `ReconcileService` (Blind-Spot net),
  reusing `GraphGitClient.DiffNameOnly`/`HashObject` (the calls removed from the
  hot path).
- New `application/verify_service.go`, `application/diagnose_service.go`,
  `application/provenance_service.go`.

### infrastructure/

- `infrastructure/graph/sha256_hasher.go` — `EventHasher` impl (peer of
  `NewUUIDSessionIDGenerator`).
- `infrastructure/graph/sqlite_repository.go` — implement `AppendEvent`,
  `HeadHash`, `StreamEvents`, `VerifyChain`; strip the baseline half of
  `CreateActionBatch` (`sqlite_repository.go:749-765`); delete
  `Get/Update/CleanupCaptureBaseline` (`:996-1067`).
- `infrastructure/graph/sqlite_client.go` — add `events` + `event_files` DDL to
  `schemaStatements` (`:332`); bump `CurrentSchemaVersion` (`:16`); remove
  `capture_baseline` DDL (`:426-430`).

### cmd/

- `cmd/capture_payload.go` — expand `claudeHookPayload` to the full PostToolUse
  schema; `mergeHookPayload` builds an `EventRecord` (after redaction).
- `cmd/capture.go` — wire `NewSHA256Hasher()` into `NewCaptureService`.
- New `cmd/graph_rebuild.go`, `cmd/graph_verify.go`, `cmd/graph_provenance.go`,
  and replace the `graph diagnose` stub.

## Schema

Add to `schemaStatements` (`sqlite_client.go:332`):

```sql
CREATE TABLE IF NOT EXISTS events (
    seq             INTEGER PRIMARY KEY AUTOINCREMENT,  -- monotonic global chain position
    event_id        TEXT NOT NULL UNIQUE,
    recorded_at     INTEGER NOT NULL,
    source          TEXT NOT NULL,                      -- claude-code|cursor|human|unknown
    instance_id     TEXT,
    kind            TEXT NOT NULL,                      -- tool|outcome|out-of-band
    hook_event_name TEXT,
    tool_name       TEXT,
    cwd             TEXT,
    transcript_path TEXT,
    permission_mode TEXT,
    payload_raw     TEXT NOT NULL,                      -- verbatim, post-redaction
    payload_size    INTEGER NOT NULL DEFAULT 0,
    truncated       INTEGER NOT NULL DEFAULT 0,
    -- outcome columns (NULL for non-outcome)
    command         TEXT,
    exit_code       INTEGER,
    exit_code_source TEXT,                              -- reported|inferred
    is_test         INTEGER NOT NULL DEFAULT 0,
    is_build        INTEGER NOT NULL DEFAULT 0,
    test_name       TEXT,
    -- chain
    prev_hash       TEXT NOT NULL,
    this_hash       TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_events_this_hash ON events(this_hash);
CREATE INDEX IF NOT EXISTS idx_events_recorded_at ON events(recorded_at);
CREATE INDEX IF NOT EXISTS idx_events_source ON events(source);
CREATE INDEX IF NOT EXISTS idx_events_instance ON events(source, instance_id);

-- Derived (Enrichment-populated; rebuildable). One row per touched file.
CREATE TABLE IF NOT EXISTS event_files (
    event_seq   INTEGER NOT NULL,
    file_path   TEXT NOT NULL,
    before_blob TEXT,                                   -- File Blob Ref (git OID)
    after_blob  TEXT,
    change_kind TEXT,                                   -- A|M|D|R
    additions   INTEGER NOT NULL DEFAULT 0,
    deletions   INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (event_seq, file_path)
);
CREATE INDEX IF NOT EXISTS idx_event_files_path ON event_files(file_path);
```

Notes:

- **`events.seq` is a global monotonic key**, distinct from the per-session
  `actions.sequence` field. `verify` and the diagnose bisect both depend on a
  single global order.
- **No diff text is stored.** The old `ActionNode.Diff` (100KB-truncated) is
  replaced by File Blob Refs in `event_files`; diffs are reconstructed on demand
  via `git diff <before_blob> <after_blob>`. This is unbounded-size-safe and keeps
  the hashed unit small.
- `events` is append-only by discipline: no code path issues `UPDATE`/`DELETE`/
  `INSERT OR REPLACE` against it (enforced by a test, see `bdd-specs.md`).

## Claude PostToolUse Payload Mapping

Current real PostToolUse stdin shape:

```json
{ "session_id", "transcript_path", "cwd", "hook_event_name": "PostToolUse",
  "permission_mode", "tool_name", "tool_input": {...}, "tool_response": {...} }
```

`claudeHookPayload` (`cmd/capture_payload.go:11-14`) expands to decode all of the
above. `tool_input`/`tool_response` are redacted, then folded into `payload_raw`.
The "unknown fields ignored / never error" rule (`:7-9`, `:28-30`) is preserved —
a PostToolUse hook must never block the agent. For other agents (cursor/windsurf)
that don't send this shape, capture degrades to the Reconciliation net.

| Claude field | EventRecord | column |
|---|---|---|
| `session_id` | `InstanceID` | `instance_id` |
| `transcript_path` | `TranscriptPath` | `transcript_path` |
| `cwd` | `Cwd` | `cwd` |
| `hook_event_name` | `HookEventName` | `hook_event_name` |
| `permission_mode` | `PermissionMode` | `permission_mode` |
| `tool_name` | `ToolName` | `tool_name` |
| `tool_input` + `tool_response` (redacted) | `PayloadRaw` | `payload_raw` |

## Hash Chain

`this_hash = SHA256( prev_hash ‖ "\n" ‖ CanonicalForm(e) )`, genesis `prev_hash`
= 64 hex zeros for `seq` 1.

**Canonical Form** is the synthesis of the two research recommendations (avoid JSON
re-serialization drift *and* cover scalar fields):

```
CanonicalForm(e) =
    chain_version_byte ‖
    le_uint64(seq) ‖ le_uint64(recorded_at) ‖
    len_prefixed(source) ‖ len_prefixed(instance_id) ‖
    len_prefixed(kind) ‖ len_prefixed(tool_name) ‖
    le_int(exit_code_or_sentinel) ‖
    sha256(payload_raw)
```

- Scalar fields are length-prefixed and fixed-order — **no JSON ordering or
  whitespace ambiguity**.
- The variable payload is covered by hashing its **exact stored bytes**
  (`payload_raw`), so re-serialization can never make `verify` fail on untampered
  data. (`mergeHookPayload` must retain the raw bytes from `readPipedStdin` after
  redaction splicing — today it unmarshals and discards them.)
- `chain_version_byte` freezes v1 forever; a future canonicalization change adds a
  new version, never mutates v1, so historical rows always re-verify.
- Everything immutable enters the hash; `prev_hash`/`this_hash`/`seq`-as-rowid are
  excluded from `CanonicalForm` itself (seq is folded in as a field value).

**Concurrent append safety:** read-head → compute → insert must be one
`BEGIN IMMEDIATE` transaction; otherwise two writers read the same head and **fork**
the chain. `modernc.org/sqlite` with `SetMaxOpenConns(1)` + `_txlock=immediate`
gives the single-writer serialization for free. There is **one chain per repo**;
concurrent agents are distinguished by `instance_id` at the Projection layer, not by
separate chains.

New `GraphRepository` methods (`domain/graph/repository.go:5`):

```go
AppendEvent(ctx, EventRecord) (EventRecord, error) // only writer into events; assigns seq + this_hash
HeadHash(ctx) (string, error)                      // current chain head (prev_hash for next)
StreamEvents(ctx, sinceSeq int64) (EventCursor, error)
VerifyChain(ctx) (VerifyResult, error)
```

The three `CaptureBaseline` methods (`repository.go:54-58`) are removed.

## Projections & Rebuild

`ProjectionRebuilder.Rebuild(ctx)` = what `graph rebuild` runs:

1. `repo.VerifyChain` first; fail loud on a `ChainBreak`.
2. Reset the derived tables (`sessions`, `actions`, `action_modifies`,
   `action_produces`, `event_files`).
3. `StreamEvents(ctx, 0)` in `seq` order, folding:
   - **sessions** — group by `(source, instance_id)`; close a session when the gap
     between consecutive Events exceeds `sessionTimeoutMins` (`capture_service.go:11`).
     Same semantics as today's `GetActiveSession`, now derived. Because
     `instance_id` = Claude `session_id`, sessions map 1:1 to Claude sessions.
   - **actions** — one row per Event; `sequence` = per-session running counter
     (reproducing `CreateActionBatch`'s `MAX(sequence)+1` deterministically from
     global `seq`).
   - **action_modifies / event_files** — file paths and old/new content come from
     `tool_input` (Edit/Write → `file_path` + `old_string`/`new_string`; MultiEdit →
     each edit). Additions/deletions computed from the payload; File Blob Refs
     (`before_blob`/`after_blob`) computed here in the cold path via `HashObject`.
   - **action_produces** — preserved via the existing
     `GraphActionLinker.LinkActionsToCommit` seam at commit time.
4. `Timeline` (`sqlite_repository.go:797`) and `UnlinkedActionsForFiles` (`:944`)
   keep working unchanged — their input tables are now Projections.

Determinism: projections derive ordering and timestamps **solely** from Event
fields + chain order (no wall-clock, no map iteration order), enforced by the
byte-identical rebuild test.

Incremental Enrichment uses a stored cursor (watermark) so the common case consumes
only the tail; full Replay-from-genesis happens only on projection schema change or
corruption.

## Reconciliation (Blind-Spot net)

`ReconcileService` re-introduces diff-based detection, but only as a fallback, not
the primary path:

- Compare current working-tree file hashes against the state implied by Replaying
  Events to HEAD. Any file whose content diverges from what the log accounts for →
  append a synthetic `EventRecord{Source:"unknown", Kind:"out-of-band",
  ToolName:"external-edit"}` to the **same chain** (so it is tamper-evident and
  replayable), carrying `before_blob` = last-known-after, `after_blob` = current.
- Reconcile only the **unexplained residual** (files changed but not covered by
  recent Events) to avoid double-counting hook-captured edits.
- Hook points: (1) `graph rebuild` / `EnsureIndex` before serving queries;
  (2) at commit time alongside `LinkActionsToCommit`, so every byte that reaches a
  commit is attributable to either an observed Event or an explicit Out-of-Band
  Event.

## Audit Surface

### `graph verify` → `VerifyResult`

Walk `events ORDER BY seq`; recompute each `this_hash`; track `expected_prev`.
Classify the first break by *which invariant fails first*, using a linkage walk
(follow `prev_hash` pointers from genesis) compared against `seq` order:

| First failing invariant | `ChainBreak.kind` |
|---|---|
| self-hash mismatch, linkage intact, seq contiguous | `ROW_EDITED` |
| `seq` gap + linkage walk terminates early (missing link target) | `ROW_DELETED` |
| table row not reachable from the genesis linkage walk | `ROW_INSERTED` |
| every self-hash recomputes but linkage order ≠ seq order | `ROW_REORDERED` |

`VerifyResult`: `{status, events_total, events_verified, first_break: ChainBreak{seq,
event_id, kind, expected_this_hash, stored_this_hash, ...}}`. Exit code **4** =
chain integrity failure (distinct from the existing `3` = auto-index failure).
Read-only; safe under WAL during an active writer.

### `graph provenance <file>` → Provenance View

Chronological, rename-aware (`ResolveRenames`) merge of (a) Event-log changes for
the file via `event_files`, and (b) Out-of-Band Events. Each row:
`{seq, when, who(source), tool, before_blob→after_blob, linked_commit?}`. Out-of-Band
rows are flagged. A clean `verify` means the provenance is complete and untampered.

## Forensic Surface

### `graph diagnose "<symptom>"` → `DiagnosisResult`

Replaces the P2 stub (`cmd/diagnose.go`). Steps (a)-(c) deterministic; (d) re-ranks
only the top-N.

1. Run `verify` internally; refuse (exit 4) on a break unless `--force`.
2. **Suspect Window** from Outcome Events: `last_green = MAX(seq)` test outcome with
   `exit_code==0` matching the symptom; `first_red = MIN(seq) > last_green` with
   `exit_code!=0`. Window = Events strictly between them. No green baseline →
   window opens at genesis, flagged `low_confidence: no_green_baseline`.
3. **Relevant file set R**: seeds (from `--file`, the symptom string, the failing
   test's session) expanded by `ImpactService.Impact({Paths:seeds, Depth:1})`
   (reuses `CouplingStrength`).
4. **Candidates** = window Events touching R (rename-resolved), scored:

   ```
   score(e) = 0.35*recency + 0.25*impact_overlap + 0.25*direct_seed_hit
            + 0.10*churn   - 0.05*later_reverted
   recency = (e.seq - last_green) / (first_red - last_green)
   ```

   Total order, ties broken by higher `seq` — fully reproducible.
5. **Optional LLM re-rank** (`--llm`): only the top-N candidates, each with its
   reconstructed candidate diff (`git diff before_blob after_blob`) + symptom. The
   LLM may reorder within N but cannot add candidates or change exit behavior.
   `--no-llm` (or no endpoint configured) → deterministic order is final.

`DiagnosisResult`: baseline (green/red seq + commit), window size, ranked
Candidates (each with `before_blob`/`after_blob` for direct diffing),
`chain_verified`.

### Outcome Event capture

When `tool_name == "Bash"`: parse `tool_input.command`; classify test/build
deterministically (`go test`, `make test`, `go build`, `pnpm test`, `pytest`,
`cargo test`, ...); extract `test_name` from `-run`/package path. `exit_code` from
`tool_response` when present (`exit_code_source = "reported"`), else inferred from
stderr/failure markers (`exit_code_source = "inferred"`, down-weighted in
diagnose). **Honest limits:** PostToolUse fires only for instrumented agents
(manual/CI runs produce no Outcome Events → coarse baselines, hence the
`low_confidence` flag and Out-of-Band net); compound commands (`go build && go test`)
expose only an aggregate exit. A parse failure **drops** the Outcome Event rather
than failing the hook — the log can have missing Outcome Events, never fabricated
ones.

## Integration Points

**Changes:** `cmd/capture_payload.go` (full schema + EventRecord build),
`cmd/capture.go` (wire hasher), `application/capture_service.go` (rewrite to
append-only), `domain/graph/repository.go` (+AppendEvent/HeadHash/StreamEvents/
VerifyChain, −CaptureBaseline), `domain/graph/session.go` (+event.go, −CaptureBaseline),
`infrastructure/graph/sqlite_client.go` (+events/event_files DDL, −capture_baseline,
bump version), `infrastructure/graph/sqlite_repository.go` (+chain ops, −baseline).
New: `application/{projection,reconcile,verify,diagnose,provenance}_service.go`,
`infrastructure/graph/sha256_hasher.go`, `cmd/graph_{rebuild,verify,provenance}.go`.

**Untouched (the code index):** `commits`/`files`/`authors`/`modifies`/`authored`/
`co_changed`/`renames` (`sqlite_client.go:334-389`); `UpsertCommit`/`CreateModifies`/
`RecomputeCoChanged`/`IncrementalCoChanged`/`Impact`/`ResolveRenames`; the entire
AST layer + FTS5; `indexer.go`. These index git history and source structure, not
agent behavior.

## Exit Codes

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | general error |
| 2 | hook blocked commit (existing) |
| 3 | auto-index failure (existing) |
| 4 | chain integrity failure (`verify`/`diagnose` on a `ChainBreak`) |
