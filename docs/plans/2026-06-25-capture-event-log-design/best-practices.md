# Best Practices — Capture Event Log

Security, performance, testing, and pitfalls for the payload-observed, hash-chained
capture redesign. Canonical vocabulary per `_index.md` Glossary.

> **Assumption that no longer holds.** The old subsystem's stance ("No secrets in
> graph; diffs come from `git diff` which respects `.gitignore`") is void under this
> redesign: capturing the raw hook payload deliberately ingests content `git diff`
> never surfaces (`.env` contents, inline secrets in Bash commands). **Redaction
> moves from "not needed" to load-bearing.**

## 1. Security

### 1.1 Secret Redaction (FR12)

Layered, best-effort, honest. v1 ships A + B(small) + size cap; deeper detection is
future hardening.

- **Layer A — path denylist (fast, runs first, no scanning).** Inspect structured
  fields (`tool_input.file_path` and similar). Sensitive paths — `.env*`, `*.pem`,
  `*.key`, `*.p12`, `*.pfx`, `id_rsa`, `id_ed25519`, `*.keystore`, `credentials`,
  `.npmrc`, `.netrc`, `*.tfvars` — store a **Redaction Digest** (`sha256` + length),
  not contents.
- **Layer B — inline token regex (free-form fields).** For `Bash.command`, file
  contents, and `tool_response`, run a small **compiled-once** regex set for common
  formats: AWS `(AKIA|ASIA)[A-Z0-9]{16}`, GitHub `ghp_[0-9A-Za-z]{36}`, Slack
  `xox[baprs]-...`, PEM `-----BEGIN ... PRIVATE KEY-----`. Replace each hit with a
  **Typed Placeholder** `[REDACTED:<rule-id>]` (preserves *kind* for forensics
  without leaking value). **Do not shell out** to gitleaks/trufflehog (network +
  process-spawn cost is unusable in the hot path).
- **Layer C (future hardening, out of v1):** embed the gitleaks detector library +
  an entropy fallback for novel secrets. Noted, not built — YAGNI for v1.
- **Size cap:** any payload field over the cap → Redaction Digest + truncation flag.

The strongest mitigation is **never storing the bytes** — path-deny + size-cap →
Redaction Digest. Document plainly that token regex is best-effort and will miss
novel formats.

### 1.2 Tamper-evidence altitude (NFR1)

`graph.db` is user-writable. State the truth without overselling:

> A plain unkeyed SHA-256 chain detects accidental corruption and naive edits (an
> editor who forgets to re-chain forward). It does **not** stop the file's own owner:
> they can edit Event N and recompute every subsequent `prev_hash` to the head, and
> `verify` then passes. The chain is **tamper-evident, not tamper-proof**.

Ladder, with v1 choice and future hardening:

| Mechanism | Catches | git-agent |
|---|---|---|
| Plain SHA-256 chain | corruption, naive edit | **v1 (ship)** |
| HMAC with OS-keychain key | a file-writer, if key is off-DB | future opt-in |
| External head-hash anchoring (remote/printed/keychain) | a fully recomputed chain | future opt-in |
| Merkle tree | many-verifier transparency proofs | rejected — overkill for a serial local log |

Reference ceiling: systemd-journald FSS (forward-secure HMAC + off-box verification
key). Document as aspirational, not the v1 default.

### 1.3 Canonical Form (NFR5)

The chain is only verifiable if the hashed bytes are reproducible. Two failure
modes the design avoids:

- **Re-serialization drift.** `{"a":1,"b":2}` vs `{"b":2,"a":1}` hash differently;
  re-encoding with different map order/whitespace/float formatting makes `verify`
  fail with no tampering. **Mitigation:** hash the **exact stored `payload_raw`
  bytes** for the variable part; never re-serialize before re-verifying. This means
  `mergeHookPayload` must **retain the raw stdin bytes** (after redaction splicing)
  — today it unmarshals and discards them.
- **Excluded-field mutability.** Any immutable field not entering the hash is
  silently editable. **Mitigation:** the fixed-order, length-prefixed scalar header
  (`seq`, `recorded_at`, `source`, `instance_id`, `kind`, `tool_name`, `exit_code`)
  + `sha256(payload_raw)` covers everything; only `prev_hash`/`this_hash` are
  excluded by construction.

Freeze Canonical Form v1 with a `chain_version` byte. A future format is a new
version, never a mutation of v1 — otherwise historical rows can never re-verify.
Full RFC 8785 (JCS) is unnecessary given the bytes-hash approach, and was rejected.

### 1.4 File Blob Ref vs Redaction Digest

Two distinct uses of hashes — keep them named apart:

- **File Blob Ref** (`before_blob`/`after_blob`, git OIDs in `event_files`):
  reconstruct diffs on demand (`git diff <before> <after>`) and serve provenance.
  Unbounded-size-safe; computed in the cold path.
- **Redaction Digest** (`sha256` + length): stored *in place of* secret/oversized
  content. A privacy mechanism, not a diff index.

## 2. Performance

Budget unchanged in spirit: capture must be fast and **never block/error the
agent**. `runCapture` already swallows errors (warn to stderr, return nil) —
preserve that.

### 2.1 Sub-ms, non-blocking Append (NFR3)

- **Decouple Append (hot) from Enrichment (cold)** — transactional-outbox /
  event-sourcing split. The hot path does ONE thing: insert the minimal Event row
  (`seq`, `prev_hash`, `this_hash`, `recorded_at`, `source`, `instance_id`, `kind`,
  `tool_name`, `payload_raw`). **No** `git diff`, `git hash-object` per file, AST,
  or projection updates in the hook — all of that (incl. File Blob Refs and the
  `sessions`/`actions`/`event_files` projections) moves to the cold Enrichment pass.
- **SQLite (modernc.org/sqlite, pure Go):**
  `file:graph.db?_txlock=immediate&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(50)`,
  `db.SetMaxOpenConns(1)`. Set pragmas via the connection string (not a bare `Exec`,
  which affects one pooled conn).
- **`_txlock=immediate`** makes `BeginTx` issue `BEGIN IMMEDIATE`, taking the write
  lock up front so the busy decision happens at one defined point. Deferred txns
  that upgrade read→write fail `SQLITE_BUSY` regardless of `busy_timeout`.
- **Silent skip on contention.** With a single-INSERT txn the contention window is
  sub-ms; on a rare `SQLITE_BUSY`, warn to stderr and skip the Event, exit 0 (FR13).
- **`synchronous=NORMAL` + WAL** = no per-commit fsync (fast), corruption-safe (only
  the last un-checkpointed Events can be lost on power loss — acceptable for a
  per-tool-call log). Never FULL (re-adds per-append fsync).
- **`this_hash` cost** is one SHA-256 over a small header + a 32-byte payload digest
  — tens of microseconds. The only DB read on the hot path is the head `prev_hash`,
  done inside the same `BEGIN IMMEDIATE` txn as the insert.

### 2.2 Concurrent append safety

Read-head → compute → insert MUST be one `BEGIN IMMEDIATE` transaction; otherwise
two writers read the same head and fork the chain (`SELECT ... FOR UPDATE` on the
head row does not prevent a concurrent INSERT). The single-writer model
(`SetMaxOpenConns(1)` + `BEGIN IMMEDIATE`) serializes appends. **One chain per
repo**; concurrent agents are distinguished by `instance_id` at the Projection
layer, not by separate chains.

### 2.3 Enrichment / Rebuild

A separate `graph rebuild` / cold pass reads un-enriched Events past a stored cursor
(watermark), computes File Blob Refs + diffs/stats, and incrementally maintains the
Projections. Append-only is what keeps incremental maintenance cheap — UPDATE/DELETE
on Events would force full Replay. Full Replay-from-genesis only on projection
schema change or corruption. Checkpoint the WAL (`wal_checkpoint(TRUNCATE)`) from
the cold path; avoid long-lived readers pinning the WAL end-mark.

## 3. Testing

- Follow the project's `.feature`-first BDD flow; each scenario in `bdd-specs.md`
  gets a RED test observed failing for the right reason before implementation.
- **Tamper tests** mutate `graph.db` directly (edit/delete/insert/reorder rows) and
  assert the `ChainBreak.kind` and exit code 4.
- **Determinism test:** rebuild twice, assert byte-identical projections.
- **Append-only guard:** a test asserting no production path issues UPDATE/DELETE on
  `events` (e.g. grep the repo + a runtime trigger in tests).
- **Redaction tests:** known secret formats → Typed Placeholder; sensitive path →
  Redaction Digest; assert the raw value never appears in `graph.db`.
- **Non-blocking test:** hold the write lock, assert capture exits 0 and skips.
- **e2e:** `TestMain` rebuilds the binary; re-run `go test ./e2e/...` after source
  changes (stale binary won't reflect changes).
- **Scope-boundary regression:** existing `impact`/index e2e tests must pass
  unchanged.

## 4. Common Pitfalls

1. **Discarding raw bytes then re-serializing for the hash** — map-order/whitespace
   drift breaks `verify` on untampered data. Retain and hash the exact stored bytes.
2. **Excluding fields from the hash** — `recorded_at`/`source`/`seq` not in the hash
   = silently mutable. Include every immutable field; freeze the format version.
3. **Treating an unkeyed chain as a security control** — it is corruption/naive-edit
   evidence only; the DB owner can recompute it. Don't market it as tamper-proof.
4. **Doing diff/hash/AST/projection work in the hook** — blows the latency budget
   and risks blocking. Hot path = append raw only.
5. **Connection-pool concurrency bug** — missing `SetMaxOpenConns(1)` /
   `_txlock=immediate` / connection-string pragmas → `SQLITE_BUSY` storms and forked
   chains.
6. **Chain fork from non-atomic head read** — read-head and append must be one
   `BEGIN IMMEDIATE` transaction.
7. **Redaction slow in the hot path** — compile detectors once; pre-filter with
   substring gates; cap scan size. Don't shell out.
8. **Relying on path-deny alone** — a secret in a non-denylisted file/field slips
   through; run the inline token regex as a second layer.
9. **Switching off WAL/NORMAL** — autocommit FULL reintroduces per-append fsync
   latency.
10. **Unbounded WAL growth** — a long-lived reader pins the end-mark or you never
    checkpoint. Checkpoint from the cold path.
11. **One chain per agent** — multiplies complexity and loses a single verifiable
    history. One chain; distinguish agents at the Projection layer via `instance_id`.
12. **Forgetting deletion detection** — chain walking catches edits/reorders, but a
    clean re-chain after a deletion is only visible as a `seq` gap. `verify` must
    check `seq` continuity explicitly.
13. **Inferred exit codes mistaken for ground truth** — output containing "FAIL" or
    a failure swallowed by `|| true` mis-tags reds. Record `exit_code_source` and
    down-weight inferred reds in diagnose.
14. **Fabricating Outcome Events** — on a parse failure, drop the Outcome Event;
    never guess. The log may miss Outcome Events, but must never contain fabricated
    ones.
