---
name: using-git-agent
description: Operates the git-agent CLI — atomic AI commits plus co-change relations for agents, all-language, offline, no API key. Use it whenever the user wants to commit or set up git-agent; when you want to optimize commit scopes or the .gitignore (init --scope / --gitignore); when you are about to modify a feature and need the files that historically move with it, with the commits that explain the coupling (related); when deciding which tests to run after a change (related --tests); or when checking the co-change index health (status). All related and status queries are read-only and offline (no LLM, no API key); only commit and init --scope need a provider.
---

# Git Agent CLI

AI-powered Git CLI — atomic AI commits plus co-change relations for agents,
all-language, offline, no API key.

## Install

```bash
brew install GitAgentHQ/brew/git-agent
# or: go install github.com/gitagenthq/git-agent@latest
```

## Start here

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

- **Atomic commits**: splits staged changes into up to 5 logically distinct commits, each hook-validated
- **Co-change relations**: `git-agent related` mines git history for the files that move with your change — offline, language-agnostic, no API key
- **Auto-scope**: generates commit scopes from git history automatically
- **Scope & `.gitignore` optimization**: regenerates scopes from latest history (`init --scope --force`) and re-derives `.gitignore` while preserving custom rules (`init --gitignore`)
- **Hook validation**: conventional-commit validation built in; custom hooks via shell scripts
- **Config precedence**: CLI flag > Agent Session env (`PI_MODEL`, `CLAUDE_CODE_MODEL`, `CODEX_MODEL`, `MODEL`) > `git config --local` > `~/.config/git-agent/config.yml` > free shared-gateway default; `--free` forces the gateway and ignores the rest
- **Structured output**: `-o json` on every read command for scripting

## Status check

`git-agent status` reports co-change index health (last indexed commit, row
counts, db size). Read-only and offline.
