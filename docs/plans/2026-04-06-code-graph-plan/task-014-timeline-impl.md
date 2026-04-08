# Task 014: Timeline service implementation

**depends-on**: 014-test

## Description
Implement the timeline service that queries sessions and actions from the graph repository. The service delegates to `GraphRepository.Timeline()` which runs SQL queries on the sessions and actions tables with filters for source, file, and time range.

## Execution Context
**Task Number**: 014 of 018
**Phase**: P1b -- Action Tracking Pipeline
**Prerequisites**: Timeline service tests (014-test) must exist and FAIL

## BDD Scenario
```gherkin
Feature: Timeline Service Implementation

    Scenario: Timeline query delegates to repository
        Given a TimelineRequest with filters
        When TimelineService.Query is called
        Then it should pass the request to repo.Timeline()
        And return the TimelineResult directly

    Scenario: Timeline query measures execution time
        When TimelineService.Query is called
        Then TimelineResult.QueryMs should be populated
        And the value should reflect actual query duration
```

## Files to Modify/Create
- `application/timeline_service.go` -- new service file

## Steps
### Step 1: Define TimelineService struct
```go
type TimelineService struct {
    repo graph.GraphRepository
}

func NewTimelineService(repo graph.GraphRepository) *TimelineService {
    return &TimelineService{repo: repo}
}
```

### Step 2: Implement Query method
```go
func (s *TimelineService) Query(ctx context.Context, req graph.TimelineRequest) (*graph.TimelineResult, error) {
    // Apply defaults
    if req.Top <= 0 {
        req.Top = 50
    }
    return s.repo.Timeline(ctx, req)
}
```

The heavy lifting (SQL joins on sessions + actions + action_modifies, filtering by source/file/since/top) is in `GraphRepository.Timeline()` which was defined in the repository interface (task 002) and implemented in SQLite (task 003).

### Step 3: Run tests
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
go test ./application/... -run TestTimelineService -count=1 -v
```

## Verification Commands
```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli
go test ./application/... -run TestTimelineService -count=1 -v
go vet ./application/...
```

## Success Criteria
- All timeline service tests from 014-test pass (Green phase)
- Service applies default `Top = 50` when not specified
- Service delegates filtering to repository layer (no SQL in application layer)
- `TimelineResult` fields populated correctly: Sessions, TotalSessions, TotalActions, QueryMs
- `go vet ./application/...` passes
