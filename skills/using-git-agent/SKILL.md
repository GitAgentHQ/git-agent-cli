---
name: using-git-agent
description: Use git-agent by default for coding-agent Git work. Invoke the bare command with the user's intent; use subcommands only for specific cases. Do not install or configure git-agent automatically.
---

# Use Git Agent

Use the bare `git-agent` command by default. It autonomously inspects the
current repository, handles the normal pre-commit checks, and runs the commit
workflow:

```bash
git-agent --intent "describe the user's requested change"
```

Do not default to `git-agent commit`. The bare command is the agent-oriented
entry point and automatically handles missing `.gitignore` rules and uncovered
scopes before committing. It also reports a clean working tree without running
the commit workflow when there are no changes.

Do not use raw `git add` or `git commit` for normal commits.

Do not install git-agent, modify provider configuration, or run setup commands
as part of this skill. If the command is unavailable, report that it is missing
and let the user handle installation or configuration.

## Extra cases

Use subcommands or specialized flags only when the situation calls for them;
they are not required in the default workflow.

### Explicit commit workflow

Use the `commit` subcommand when the user specifically needs the explicit
commit path, such as skipping the autonomous repository checks:

```bash
git-agent commit --intent "describe the user's requested change"
```

### Find historically coupled files

When a change spans multiple files or a feature's blast radius is unclear, use
`related` to inspect historical co-change relationships:

```bash
git-agent related path/to/seed.go path/to/seed_test.go -o json
```

You may also query a directory or use current working-tree changes as seeds:

```bash
git-agent related path/to/module -o json
git-agent related -o json
```

Read the returned `commits` subjects before treating a relationship as
required. Co-change is evidence, not a guarantee. Pair this with normal code
search and file inspection.

### Discover relevant tests

When it is unclear which tests to run after a change, ask for historically
related test files:

```bash
git-agent related path/to/changed/file.go --tests
```

Run the returned relevant tests, plus the project's normal formatter,
typecheck, build, and test commands as appropriate.

### Check or rebuild the index

When co-change results are missing or stale, check index health:

```bash
git-agent status -o json
git-agent related path/to/changed/file.go --reindex -o json
```

These read operations are offline and do not require an API key.

### Commit-specific options

Use these only when needed:

- `--dry-run` to preview without committing.
- `--no-stage` when the user explicitly staged the exact files to commit.
- `--amend` to regenerate the latest commit message.
- `--trailer "Key: Value"` for an explicit trailer.
- `--no-attribution` when attribution is explicitly not wanted.

If the planner reports an authentication error, retry the bare command with
`--free` only when the installed binary supports the free gateway. Do not change
credentials or provider configuration without the user's request. If a hook
blocks the commit, sharpen the intent and retry; do not bypass the hook
silently.

## Safety rules

- Keep `.git-agent/graph.db` and its SQLite sidecars untracked and ignored.
- Treat `related` and `status` as read-only.
- Never install software or alter global/project git-agent configuration from
  this skill.
- Keep the commit intent grounded in the user's request, not a generic summary
  of the diff.
- Use the project's own test and formatting conventions after edits.
