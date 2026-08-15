---
name: using-git-agent
description: Use git-agent by default for coding-agent Git work. The bare git-agent command is the normal autonomous workflow; use subcommands only for specific cases. Do not install or configure git-agent automatically.
---

# Git Agent CLI

AI-powered Git CLI — atomic AI commits plus co-change relations for agents,
all-language, offline, no API key.

## Start here

Use the bare command by default. Pass the user's intent directly to
`git-agent`:

```bash
git-agent --intent "describe the user's requested change"
```

The bare command is the autonomous agent entry point introduced by the recent
CLI workflow. It inspects the repository and runs the commit workflow. Do not
default to `git-agent commit`; use that subcommand only for an explicit commit
case.

This file is a discovery stub, not the usage guide. Before running any
`git-agent` command, load the actual workflow content from the CLI:

```
git-agent skills get core
```

The CLI serves skill content that always matches the installed version, so
instructions never go stale.

## Available documents

| Document | Command |
|---|---|
| Main usage guide (triggers, workflows, flags, exit codes) | `git-agent skills get core` |
| Full command reference (all flags, subcommands, config scopes, hook types) | `git-agent skills get cli` |
| All available documents | `git-agent skills list` |

## Why git-agent

- **Autonomous workflow**: the bare command inspects the repository and runs the commit workflow
- **Atomic commits**: splits changes into up to 5 logically distinct commits, each hook-validated
- **Co-change relations**: `git-agent related` mines git history for the files that move with your change — offline, language-agnostic, no API key
- **Auto-scope**: generates commit scopes from git history automatically
- **Scope & `.gitignore` optimization**: regenerates scopes from latest history (`init --scope --force`) and re-derives `.gitignore` while preserving custom rules (`init --gitignore`)
- **Hook validation**: conventional-commit validation built in; custom hooks via shell scripts
- **Config precedence**: CLI flag > `git config --local` > `~/.config/git-agent/config.yml` > free shared-gateway default; `--free` forces the gateway and ignores the rest. Agent-session env vars (`PI_MODEL`, `CLAUDE_CODE_MODEL`, `CODEX_MODEL`) never set the inference model — it resolves only from flag / local git config / user config; the session model is read separately for `Co-Authored-By` attribution
- **Structured output**: `-o json` on every read command for scripting

## Status check

`git-agent status` reports co-change index health (last indexed commit, row
counts, db size). Read-only and offline.
