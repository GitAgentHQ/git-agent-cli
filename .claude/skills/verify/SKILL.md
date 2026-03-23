---
name: verify
description: Run the full test suite (all packages including e2e) via `make test`. Use after making changes to confirm nothing is broken.
---

Run the full test suite:

```bash
make test
```

This runs `go test -count=1` across `./application/...`, `./domain/...`, `./infrastructure/...`, `./cmd/...`, and `./e2e/...`.

The `-count=1` flag disables caching so results are always fresh. The `e2e` package builds the `git-agent` binary once at the start — any code changes made before running this skill will be compiled in automatically.

If a specific package is failing, re-run just that package:

```bash
go test ./application/...
go test ./infrastructure/config/...
go test ./e2e/...
```

To run a single test by name:

```bash
go test ./application/... -run TestCommitService_NoStagedChanges
```

Report back with the test output, highlighting any failures and their likely cause.
