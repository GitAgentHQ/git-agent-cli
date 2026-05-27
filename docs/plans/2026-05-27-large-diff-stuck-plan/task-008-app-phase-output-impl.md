# Task 008 — Application phase output (impl)

| Field | Value |
|---|---|
| **subject** | Promote 8 `s.vlog` sites to `s.out`; rephrase LLM-attempt line with group context |
| **type** | impl |
| **depends-on** | ["008-app-phase-output-test"] |
| **REQ refs** | REQ-003, REQ-010 |
| **layer** | application |

## Files to modify

- `application/commit_service.go` — replace `s.vlog` with `s.out` at 9 sites; rephrase one line with group context

## Promotion map

Per `architecture.md` §2.2 (verbatim list):

| Line | Before | After (always-on) |
|---|---|---|
| 242 | `auto-generating scopes...` | unchanged text, `s.out` |
| 245 | `scope generation failed (continuing without scopes): %v` | unchanged text, `s.out` |
| 274 | `planning commits...` | unchanged text, `s.out` |
| 291 | `plan has %d groups — capping to %d` | unchanged text, `s.out` |
| 298 | `unscoped groups detected — refreshing project scopes...` | unchanged text, `s.out` |
| 311 | `updated scopes: %v — re-planning...` | unchanged text, `s.out` |
| 338 | `planned %d commit(s)` | unchanged text, `s.out` |
| 416 | `calling LLM... (attempt %d/%d)` | `commit %d/%d: drafting message (attempt %d/%d)` (carry group index + total) |
| 561 | `diff truncated (%s)` (amend) | unchanged text, `s.out` |

All other existing `vlog` calls stay verbose-only.

## Implementation steps

1. At each promoted site, swap `s.vlog(req, ...)` → `s.out(req, ...)` with identical format args, except line 416 which gets the new format and needs the loop's group index + `len(plan.Groups)` total.
2. Verify no double-printing: do NOT keep the original `vlog` call as a sibling.
3. The new line 416 format requires threading `i` and `len(plan.Groups)` (or `totalGroups`) into scope inside the per-group loop; this is already available as the loop variable.

## Verification

```bash
go test -count=1 ./application/...
```

Task-008-test passes. Existing application tests stay green. Verbose-superset property holds.
