# Task 009 — Application DIFF-SYNOPSIS fallback (impl)

| Field | Value |
|---|---|
| **subject** | Insert single-file saturation check + `buildSynopsis` helper in `CommitService.Commit` |
| **type** | impl |
| **depends-on** | ["009-app-synopsis-fallback-test"] |
| **REQ refs** | REQ-007 |
| **layer** | application |

## Files to modify

- `application/commit_service.go` — insert saturation branch after `s.truncator.Truncate`; add private `buildSynopsis` function

## Interface contracts

```go
// Private helper (application/commit_service.go)
func buildSynopsis(file, statLine string, actualBytes, capBytes int) string
```

Returns the `DIFF-SYNOPSIS` block per `architecture.md` §4.1:

```
DIFF-SYNOPSIS
file: <path>
changes: +<adds> / -<dels> (stat)
note: full diff elided (<actualBytes> bytes exceeded <cap>-byte cap)
```

Stat-line parsing: extract `+<num>` and `-<num>` counts from a `git diff --stat` line of shape `" path | N +++--"`. When the parse fails, default to `+0 / -0`.

## Implementation steps

1. After the `s.truncator.Truncate` call returns (the `if didTruncate` block at ~`commit_service.go:397-399`), add a saturation check: if `didTruncate && len(genDiff.Files) == 1 && len(genDiff.Content) == maxBytes` then call `s.git.StagedDiffStat(ctx)`, parse the stat for the single file, call `buildSynopsis`, replace `genDiff.Content` with the synopsis string.
2. When `StagedDiffStat` errors, log via `s.vlog` and continue on the truncated path (do not fail the commit).
3. Emit `s.out(req, "commit %d/%d: DIFF-SYNOPSIS fallback (%s)", i+1, totalGroups, genDiff.Files[0])` when the synopsis is used.
4. Emit `s.out(req, "commit %d/%d: truncating group diff (%d bytes)", i+1, totalGroups, maxBytes)` on the multi-file path so REQ-003 sees an always-on phase line in both branches.
5. Do NOT touch `groupDiff.Content` (the raw diff handed to the hook); only `genDiff.Content`.

## Verification

```bash
go test -count=1 ./application/...
```

Task-009-test passes. Existing tests stay green.
