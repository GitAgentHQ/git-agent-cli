# Task 007: Co-change computation test (RED)

**depends-on**: task-005

## Description
Write failing tests for co-change computation. Co-change identifies files that frequently change together in the same commits. Uses SQL self-join on the modifies table for full recompute, and incremental mode for only pairs involving files from new commits.

## Execution Context
**Task Number**: 007 of 018 (test phase)
**Phase**: P0 -- Co-change + Impact + Commit Enhancement
**Prerequisites**: task-005 (IndexService working, modifies table populated)
**Parallel with**: task-006 (EnsureIndex)

## BDD Scenario
```gherkin
Feature: Co-change computation

  Scenario: Full recompute finds co-changed pairs
    Given a repository indexed with commits where:
      | commit | files modified     |
      | c1     | a.go, b.go         |
      | c2     | a.go, b.go, c.go   |
      | c3     | a.go, b.go         |
      | c4     | d.go               |
    When I run full co-change computation
    Then co_changed contains (a.go, b.go) with co_count=3
    And co_changed contains (a.go, c.go) with co_count=1
    And co_changed contains (b.go, c.go) with co_count=1
    And d.go has no co-change entries (only appeared alone)

  Scenario: Coupling strength calculated correctly
    Given co_changed entry (a.go, b.go) with co_count=3, commits_a=3, commits_b=3
    Then coupling_strength = 3 / max(3, 3) = 1.0

  Scenario: Coupling strength with asymmetric counts
    Given co_changed entry (a.go, c.go) with co_count=1, commits_a=3, commits_c=1
    Then coupling_strength = 1 / max(3, 1) = 0.333

  Scenario: Min count filter excludes low-frequency pairs
    Given co_changed entries with various co_counts
    When I query with min_count=3
    Then only pairs with co_count >= 3 are returned

  Scenario: Incremental recompute updates only affected pairs
    Given an indexed repository with existing co_changed data
    And a new commit modifying files x.go and a.go
    When I run incremental co-change computation for new commits
    Then co_changed pairs involving x.go or a.go are recomputed
    And co_changed pairs NOT involving x.go or a.go are unchanged

  Scenario: Self-join excludes self-pairs
    Given commits where a.go is the only file modified
    When I run co-change computation
    Then no entry (a.go, a.go) exists in co_changed

  Scenario: Pair ordering is canonical
    Given co-changed files a.go and b.go
    Then the entry is stored as (a.go, b.go) where file_a < file_b
    And no duplicate (b.go, a.go) entry exists
```

## Files to Modify/Create
- `application/graph_cochange_test.go` -- all test functions

## Steps
### Step 1: Write test helpers
Set up indexed repositories with known commit/file patterns for predictable co-change results.

### Step 2: Write test functions
- `TestCoChange_FullRecompute`
- `TestCoChange_CouplingStrength`
- `TestCoChange_AsymmetricStrength`
- `TestCoChange_MinCountFilter`
- `TestCoChange_Incremental`
- `TestCoChange_NoSelfPairs`
- `TestCoChange_CanonicalOrdering`

### Step 3: Verify tests fail
```bash
go test ./application/... -run TestCoChange -v
```

## Verification Commands
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
go test ./application/... -run TestCoChange -v 2>&1 | grep FAIL
# Tests MUST fail -- Red phase
```

## Success Criteria
- Test file compiles
- All tests FAIL (Red phase)
- Tests cover: full recompute, coupling strength, min count filter, incremental, no self-pairs, canonical ordering
- Coupling strength formula: `co_count / max(commits_a, commits_b)`, min_count default=3
