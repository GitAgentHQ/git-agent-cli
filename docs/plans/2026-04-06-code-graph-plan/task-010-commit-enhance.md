# Task 010: Commit co-change enhancement

**depends-on**: task-006, task-007

## Description
Invisibly enhance the commit planning phase with co-change hints. If graph.db exists, query co-change data for staged/unstaged files and inject hints into the planning prompt. Zero new flags, zero new CLI concepts. Graceful degradation: if graph.db missing or any error, commit proceeds normally.

## Execution Context
**Task Number**: 010 of 018
**Phase**: P0 -- Co-change + Impact + Commit Enhancement
**Prerequisites**: task-006 (EnsureIndex), task-007 (co-change computation)

## BDD Scenario
```gherkin
Feature: Invisible commit co-change enhancement

  Scenario: Co-change hints injected into planning when graph.db exists
    Given a repository with graph.db containing co-change data
    And files a.go and b.go have coupling_strength 0.78
    When I run git-agent commit with changes to a.go and c.go
    Then the planning prompt includes:
      """
      Files that frequently change together (consider grouping):
      - a.go <-> b.go (78%)
      """
    And commit proceeds normally

  Scenario: No graph.db -- commit works without enhancement
    Given a repository with no graph.db
    When I run git-agent commit
    Then commit proceeds normally with no error
    And no co-change hints are in the planning prompt

  Scenario: Graph error -- commit still works
    Given a repository with a corrupted graph.db
    When I run git-agent commit
    Then commit proceeds normally with no error
    And verbose output logs the graph error

  Scenario: EnsureIndex runs before co-change query
    Given a repository with outdated graph.db
    When I run git-agent commit
    Then EnsureIndex updates the graph first
    Then co-change hints reflect the latest data

  Scenario: No co-change data for staged files
    Given a repository with graph.db but no co-change entries for staged files
    When I run git-agent commit
    Then PlanRequest.CoChangeHints is empty (not nil)
    And planning proceeds without the co-change section in the prompt

  Scenario: CoChangeHints field flows through to OpenAI client
    Given PlanRequest has CoChangeHints with 2 entries
    When the OpenAI client builds the plan prompt
    Then planParts includes a "Files that frequently change together" section
    And each hint is formatted as "- fileA <-> fileB (NN%)"
```

## Files to Modify/Create
- `application/commit_service.go` -- add optional `coChangeProvider`, query before `s.planner.Plan()` (~line 269)
- `infrastructure/openai/client.go` -- append co-change section to planParts (~line 417)

## Steps
### Step 1: Add coChangeProvider to CommitService
```go
// Optional -- nil means skip co-change enhancement (no error).
type CoChangeProvider interface {
    CoChangeHintsForFiles(ctx context.Context, repoPath string, files []string) ([]commit.CoChangeHint, error)
}
```
Add as optional field on CommitService. Wire in `cmd/commit.go` only if graph feature is available.

### Step 2: Modify CommitService.run (around line 269)
Before the call to `s.planner.Plan()`:
```go
if s.coChangeProvider != nil {
    hints, err := s.coChangeProvider.CoChangeHintsForFiles(ctx, repoPath, allFiles)
    if err != nil {
        s.vlog(req, "co-change lookup failed (proceeding without): %v", err)
    } else {
        planReq.CoChangeHints = hints
    }
}
```

### Step 3: Modify OpenAI client Plan method (around line 417)
After existing planParts assembly:
```go
if len(req.CoChangeHints) > 0 {
    var lines []string
    for _, h := range req.CoChangeHints {
        lines = append(lines, fmt.Sprintf("- %s <-> %s (%d%%)", h.FileA, h.FileB, int(h.Strength*100)))
    }
    planParts = append(planParts, "Files that frequently change together (consider grouping):\n"+strings.Join(lines, "\n"))
}
```

### Step 4: Implement CoChangeProvider adapter
Create a thin adapter that wraps EnsureIndexService + CoChange query:
1. Check if graph.db exists -- if not, return nil, nil
2. EnsureIndex (incremental)
3. Query co-change for the given files
4. Convert to []commit.CoChangeHint
5. On any error, return nil, err (caller logs and continues)

### Step 5: Wire in cmd/commit.go
Construct CoChangeProvider and pass to CommitService. If construction fails (e.g., can't open DB), set to nil.

## Verification Commands
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
make build
make test
# Verify co-change provider is optional
go test ./application/... -run TestCommitService -v
# Verify prompt includes co-change section
go test ./infrastructure/openai/... -run TestPlan -v
```

## Success Criteria
- CommitService has optional `coChangeProvider` field (nil = skip, no error)
- Co-change hints injected into PlanRequest.CoChangeHints before `s.planner.Plan()`
- OpenAI client appends co-change section to planParts when hints are non-empty
- Format: `"- fileA <-> fileB (NN%)"` under `"Files that frequently change together (consider grouping):"`
- Graceful: missing graph.db or errors do not block commit (logged to verbose)
- Zero new CLI flags or user-facing concepts
- `make build` and `make test` pass
