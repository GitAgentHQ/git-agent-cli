# Task 012: Capture service implementation

**depends-on**: 012-test

## Description
Implement the delta-based capture service that records agent actions into the graph. The service compares file content hashes against a `capture_baseline` table to detect true deltas, ensuring only newly changed files are attributed to each action. Sessions are scoped by source + instance_id with a 30-minute timeout.

## Execution Context
**Task Number**: 012 of 018
**Phase**: P1b -- Action Tracking Pipeline
**Prerequisites**: Capture service tests (012-test) must exist and FAIL

## BDD Scenario
```gherkin
Feature: Action Capture Service Implementation

    Scenario: Delta-based capture algorithm
        Given changed files from git diff --name-only
        When Capture is called
        Then the service should:
            1. List changed files via git.DiffNameOnly
            2. Compute hash per file via git.HashObject
            3. Load capture_baseline hashes from repository
            4. Compute delta (files whose hash differs from baseline)
            5. If no delta, return CaptureResult{Skipped: true}
            6. Generate diff for delta files only via git.DiffForFiles
            7. Truncate diff at 100KB if needed
            8. Find or create Session (source + instance_id + 30min timeout)
            9. Create Action with delta diff and incremented sequence
            10. Create action_modifies rows for each delta file
            11. Update capture_baseline with current hashes for ALL changed files

    Scenario: Session lifecycle
        Given a CaptureRequest with EndSession=true
        When Capture is called
        Then the active session should be ended
        And CaptureResult should indicate skipped with reason "session ended"
```

## Files to Modify/Create
- `application/capture_service.go` -- capture service implementation

## Steps
### Step 1: Implement CaptureService struct
```go
type CaptureService struct {
    repo graph.GraphRepository
    git  graph.GraphGitClient
}
```

### Step 2: Implement Capture method
Follow the delta-based algorithm:
1. Handle `EndSession` flag first (early return)
2. Call `git.DiffNameOnly()` to get changed files
3. Return skipped if no changed files
4. Call `git.HashObject()` for each changed file
5. Call `repo.GetCaptureBaseline()` to load previous hashes
6. Compute delta: files where current hash differs from baseline
7. Return skipped if no delta files
8. Call `git.DiffForFiles()` for delta files only
9. Truncate diff at 100KB (`maxDiffBytes = 100 * 1024`)
10. Call `repo.GetActiveSession()` with 30-minute timeout
11. If no active session, create new one with `uuid.New()`
12. Get action count for sequence numbering
13. Create `ActionNode` with ID `"{sessionID}:{sequence}"`
14. Create `action_modifies` rows for each delta file
15. Update capture_baseline with current hashes for ALL changed files (not just delta)

### Step 3: Implement EndSession method
Find active session for source+instanceID, call `repo.EndSession()`.

### Step 4: Implement truncateDiff helper
Truncate at 100KB, append `"\n[truncated]"` marker.

### Step 5: Run tests
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
go test ./application/... -run TestCaptureService -count=1 -v
```

## Verification Commands
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
go test ./application/... -run TestCaptureService -count=1 -v
go vet ./application/...
```

## Success Criteria
- All capture service tests from 012-test pass (Green phase)
- Delta-based tracking works: only files with changed hashes are attributed
- Baseline is updated for ALL changed files (prevents re-attribution)
- Sessions are scoped by source + instance_id with 30-minute timeout
- Action IDs follow `"{sessionID}:{sequence}"` format
- Diffs truncated at 100KB with `[truncated]` marker
- EndSession flag correctly ends active session
- No-op when no diff or no delta
- `go vet ./application/...` passes
