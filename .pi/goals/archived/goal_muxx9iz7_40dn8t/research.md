## Key files and what they do

**`infrastructure/eval/eval.go`** — the only non-test file in the package. Its package doc says it "reads commits from a repository, reconstructs the structured state each seam would see at run time, and reports how often the judgment agrees with the recorded commit message."

- `Sample` (struct): `SHA`, `Title`, `Files []string`, `Numstat string`, `Diff string`, `DiffSize int`, `Date time.Time`.
- `MaxSampleDiffBytes = 120_000` (const): truncation cap on `Sample.Diff`.
- `ConventionalPrefix(title string) (commitType, scope string, ok bool)`: parses `type(scope)!:` from a title; lowercases; rejects empty/whitespace/unscoped-empty.
- `Collector` (struct, field `Dir string`): `Collect(ctx, limit)` runs `git log --no-merges -n <limit> --pretty=format:%H\x1f%s\x1f%cI`, then per commit `git show --name-only`, `git show --numstat`, `git show --unified=3`; sets `DiffSize` to the full-diff length and truncates `Diff` at `MaxSampleDiffBytes`. Helper methods `git`, `gitLines`, `run` (unexported).
- `Report` (struct): `Seam`, `Samples`, `Judged`, `TypeHits`, `ScopeHits`, `PrefixHits`, `TypeMisses`, `ScopeMisses`, `TypeMissConfidences`, `ScopeMissConfidences`, `Confidence`, `TypeConfidence`, `ScopeConfidence`, `Latency`, `InputTokens`, `DiffBytesTotal`.
  - Methods: `ScopeFloorHolds(floor)`, `TypeFloorHolds(floor)`, `Accuracy()` (`PrefixHits/Judged`), `String()`, `Record(...)`, `RecordParts(...)`, `Finish()`.
- `ParseNumstat(raw string) map[string][2]int`.
- Unexported `percent`, `shortSHA`.

**`infrastructure/eval/eval_test.go`** (no build tag) — unit tests: `newRepo` builds a temp repo with 3 commits + a merge; `TestCollector_ReadsOneSamplePerNonMergeCommit`, `TestCollector_HonorsTheLimit`, `TestConventionalPrefix`, `TestReport_FoldsSamplesIntoOneBlock`, `TestReport_ZeroJudgedSamplesIsZeroAccuracy`, `TestParseNumstat`.

**`infrastructure/eval/live_test.go`** (`//go:build jevlive`) — `TestLiveTypeScopeSeam`, `TestLiveScopeSeam`; helpers `liveClient`, `readScopes`, `repoLayout`, `repoLayoutOf`, `topLevelDirs`, `estimateTokens`.

**`infrastructure/eval/ab_test.go`** (`//go:build jevlive`) — `TestLiveAgainstBaseline`, `measureRepo`, `printAB`; types `abConfig`, `arm`; helpers `readScopeList`, `stagedFor`, `fileChanges`, `bucketsFor`, `groupsOf`, `sameGrouping`, `shapeOrNone`, `shape`, `percent`, `orNone`, `sha`, `trim`, `scopeMap`, `abRepos`, `repoRoot`, `absPath`, `abDelay`, `abLimit`, `repoLabel`.

**`infrastructure/eval/tech_live_test.go`** (`//go:build jevlive`) — `TestLiveTechClassifier`; helpers `recordedTechnologies`, `set`, `hostOS`, `defaultMeasuredRepos`.

**`infrastructure/typesafe/live_test.go`** also carries `//go:build jevlive`.

**`README.md`** around lines 265–300 documents the harness invocation:
- `TYPESAFE_API_KEY=... JEV_EVAL_REPO=. go test -tags jevlive -v ./infrastructure/eval/`
- `TYPESAFE_API_KEY=... AB_BASE_URL=... AB_MODEL=... go test -tags jevlive -v -run TestLiveAgainstBaseline ./infrastructure/eval/`
- `CHANGELOG.md:16` describes the harness as "replays repository history through a judgment and reports agreement with the recorded commit titles, behind the `jevlive` build tag."

## Current behavior relevant to the focus

### 1. Sample building, metric, invocation, gating

- **Samples come from git history, not a file.** `Collector.Collect` (eval.go) shells out to `git log` / `git show` in `Collector.Dir` (empty = cwd). Ground truth is the recorded commit subject (`Sample.Title`), parsed by `ConventionalPrefix`. Input state for a judgment is `Files` + `Numstat` + truncated `Diff`; `MaxSampleDiffBytes` matches the CLI's diff truncation. Merge commits are skipped (`--no-merges`). A file-based repo is only used in unit tests (`newRepo`).
- **Agreement metric:** `Report.TypeHits/ScopeHits/PrefixHits` counted against `Judged`; `Accuracy()` = `PrefixHits/Judged`; `String()` renders `percent(TypeHits,Judged)` and `percent(ScopeHits,Judged)`. `Record`/`RecordParts` also accumulate per-part mean confidence, mean latency, input tokens, `DiffBytesTotal`. Floor safety is `ScopeFloorHolds` / `TypeFloorHolds` (true only if every miss confidence is `< floor`). A/B adds baseline-vs-jev deltas for type/scope and grouping identity (`sameGrouping`).
- **Invocation:** only through `go test -tags jevlive -v ... ./infrastructure/eval/`. Env vars observed: `TYPESAFE_API_KEY` (tests skip if unset), `JEV_EVAL_REPO`, `AB_REPO`, `AB_LIMIT`, `AB_DELAY`, `AB_API_KEY`, `AB_BASE_URL`, `AB_MODEL`, `AB_OS`.
- **Live gating:** `//go:build jevlive` on `live_test.go`, `ab_test.go`, `tech_live_test.go`, and `infrastructure/typesafe/live_test.go`, so `go test ./...` never calls a model. Each live test additionally `t.Skip`s when `TYPESAFE_API_KEY` is empty.
- **Repository-state reconstruction:** none found. `eval.go` only reads commit metadata/diff into in-memory `Sample` values. The closest thing is `stagedFor(s eval.Sample) *diff.StagedDiff` in ab_test.go, which returns `&diff.StagedDiff{Files: s.Files, Content: s.Diff, Lines: ...}`; `fileChanges` and `bucketsFor` likewise build in-memory request objects. No worktree, checkout, index write, `git stash`, or archive is created anywhere in `infrastructure/eval` or the rest of the code that reads history. (Package doc's word "reconstructs" refers to the in-memory `StagedDiff`, not a repository state.)

### 2. `application/commit_service.go` — `DryRun` / `NoStage` vs staging

Field declarations:
- `CommitRequest.DryRun` — line 82; `CommitRequest.NoStage` — line 83.

Where each field is checked:
- `DryRun`:
  - `startHunkRun`, line 318: `if !splitHunksOn(req.SplitHunks) || req.NoStage || req.DryRun { return nil, nil }`.
  - `commitGroups`, line 876: `if req.DryRun { committed = append(...); ledger.Mark(group.Changes); continue }` — skips `s.git.Commit` (line 883).
  - `commitGroups`, line 898: `if len(committed) == 0 && !req.DryRun` (error only on a real run); line 901 sets `CommitResult.DryRun`.
  - `commitAmend`, line 1175–1176: early `return &CommitResult{...DryRun: true}` before `s.git.AmendCommit` (line ~1179).
- `NoStage`:
  - `startHunkRun`, line 318 (returns nil, no `HunkSnapshot`/`IndexTree`).
  - `collectChanges`, line 343: `if req.NoStage { staged := StagedDiff(); error if empty; return staged, &diff.StagedDiff{} }`. It never calls `AllChangedFiles` on this branch.

Where the index is written (all in this file):
- `prepareGroup` (func at line 924):
  - line 925: `s.git.UnstageAll(ctx)` — unconditional first step.
  - line 931: `s.git.StageChanges(ctx, hunks.snapshot, ledger.onTopOf(group.Changes))` — hunk path (`hunks != nil && !allWholeFile`).
  - line 934: `s.git.StageFiles(ctx, group.Files())` — whole-file path.
- `restoreUncommittedFiles` (line 1101):
  - line 1112: `s.git.StageFiles(context.Background(), toRestore)`.
- `startHunkRun` line 326: `s.git.IndexTree(ctx)` records the pre-run index tree (a read, not a write); `restoreAfterFailure` line 1092 `s.git.RestoreIndexTree(...)` restores it, but only when `commitGroups` returns `retErr != nil` (deferred at lines 800–804).

Interaction of the two fields with staging:
- `prepareGroup` has no `DryRun` and no `NoStage` guard. It runs `UnstageAll` then a stage call for every group, before `commitGroups` reaches the `DryRun` branch at line 876.
- **Dry run:** `startHunkRun` returns nil (line 318), so staging is always whole-file via `StageFiles`. For each group `prepareGroup` calls `UnstageAll` then `StageFiles(group.Files())`; the `DryRun` branch then skips `git.Commit`. Because each iteration's `UnstageAll` clears the previous iteration's staging and nothing is committed, the index after a successful dry run holds only the files of the **last** processed group staged (whole-file), not the pre-run staged state. The deferred `restoreAfterFailure` runs only on `retErr != nil`, so a successful dry run does not restore the original index. (No test asserts the post-run index state; `TestCommitService_DryRun` at commit_service_test.go:337 o
…[truncated at 8000 characters.]
