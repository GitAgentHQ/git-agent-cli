In the worktree /Users/FradSer/Developer/FradSer/git-agent/git-agent-hunks (branch feat/hunk-staging), git-agent can stage and commit at hunk granularity, enabled by an opt-in setting, without changing the behavior of a run that leaves it off.

When hunk staging is on, one tracked file whose change spans two or more regions becomes separate commits: each commit's index entry holds only the hunks of its own commit, the working tree is never written to, and the existing agent-facing JSON output keeps its `files` field.

When a run ends early (hook block or error), the index is restored to the state the run started from, so a hunk-level run that fails leaves no partial staging behind.

Hunk staging is opt-in: with the setting off, every existing behavior, exit code, prompt, and JSON field is byte-identical to develop. The setting is read from the `split_hunks` config key (values never | auto | always) and from a --split-hunks flag on `git-agent commit`, and both are registered in infrastructure/config/keys.go.
