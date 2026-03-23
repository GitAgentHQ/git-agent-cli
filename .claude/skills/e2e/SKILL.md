---
name: e2e
description: Run only the end-to-end tests (builds the binary via TestMain, then exercises it as a subprocess). Use after changes that affect CLI behavior or flags.
---

Run the e2e test suite:

```bash
cd /Users/FradSer/Developer/FradSer/git-agent/git-agent-cli && go test -count=1 ./e2e/...
```

`TestMain` in `e2e/` rebuilds the `git-agent` binary before tests run, so a stale binary is not a concern here.

If a test fails, read the failing test file in `e2e/` to understand setup and assertions before investigating.
