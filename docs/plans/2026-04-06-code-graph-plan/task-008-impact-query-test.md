# Task 008: Impact query service test (RED)

**depends-on**: task-007

## Description
Write failing tests for the impact query service. This replaces the old "blast-radius" concept. Given a file path, it queries co_changed to find files that frequently change together, resolves renames, and ranks by coupling_strength. No call chain or import lookup (no AST).

## Execution Context
**Task Number**: 008 of 018 (test phase)
**Phase**: P0 -- Co-change + Impact + Commit Enhancement
**Prerequisites**: task-007 (co-change computation working)

## BDD Scenario
```gherkin
Feature: Impact query

  Scenario: Query impact for a file with co-changed partners
    Given an indexed repository with co_changed data:
      | file_a     | file_b      | co_count | coupling_strength |
      | handler.go | service.go  | 15       | 0.85              |
      | handler.go | model.go    | 8        | 0.53              |
      | handler.go | routes.go   | 4        | 0.30              |
    When I query impact for "handler.go" with top=10
    Then I receive 3 entries ranked by coupling_strength descending
    And the first entry is service.go with strength 0.85

  Scenario: Query impact respects depth parameter
    Given co_changed data forming a chain: a->b->c->d
    When I query impact for "a" with depth=1
    Then I receive only direct co-changed files of "a"
    When I query impact for "a" with depth=2
    Then I also receive co-changed files of b (transitive)

  Scenario: Query impact with rename resolution
    Given "old_name.go" was renamed to "new_name.go"
    And co_changed data references "old_name.go"
    When I query impact for "new_name.go"
    Then results include entries originally associated with "old_name.go"
    And the rename chain is included in the result

  Scenario: Query impact for file with no co-changes
    Given a file "lonely.go" with no co_changed entries
    When I query impact for "lonely.go"
    Then I receive an empty result with no error

  Scenario: Query impact respects min_count filter
    Given co_changed entries with various co_counts
    When I query impact for "handler.go" with min_count=5
    Then only entries with co_count >= 5 are returned

  Scenario: Query impact respects top-N limit
    Given a file with 50 co-changed partners
    When I query impact with top=10
    Then only the top 10 by coupling_strength are returned
```

## Files to Modify/Create
- `application/graph_impact_test.go` -- all test functions

## Steps
### Step 1: Write test functions
- `TestImpactQuery_CoChangedPartners`
- `TestImpactQuery_Depth`
- `TestImpactQuery_RenameResolution`
- `TestImpactQuery_NoCoChanges`
- `TestImpactQuery_MinCount`
- `TestImpactQuery_TopN`

### Step 2: Set up test data
Use mock or real repository with pre-populated co_changed and renames data.

### Step 3: Verify tests fail
```bash
go test ./application/... -run TestImpactQuery -v
```

## Verification Commands
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
go test ./application/... -run TestImpactQuery -v 2>&1 | grep FAIL
# Tests MUST fail -- Red phase
```

## Success Criteria
- Test file compiles
- All tests FAIL (Red phase)
- Tests cover: co-changed partners, depth, rename resolution, no co-changes, min_count, top-N
- No call chain or import-based analysis (no AST)
