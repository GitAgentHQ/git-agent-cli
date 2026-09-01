---
name: using-git-agent
description: Use git-agent by default for coding-agent Git work. The bare git-agent command is the normal autonomous workflow; use subcommands only for specific cases. Do not install or configure git-agent automatically.
---

# Git Agent CLI

AI-powered Git CLI for atomic AI commits. It is online and LLM-backed;
official releases provide a shared gateway, so no personal API key is required.

## Start here

Use the bare command by default. Every commit is two steps — gather context
first, then assemble and pass the intent. Never build the intent from memory
alone:

1. **Gather context**: use the harness session-context tool (`session_context`
   in pi) to collect the user's requests and decisions since the last commit;
   if no such tool exists, fall back to reading the diff yourself.
2. **Assemble the intent**: condense that context into one concise sentence
   describing what changed and why — never a raw file list or diff dump.
3. **Commit** with the assembled intent:

```bash
git-agent --intent "<one-sentence intent assembled from the gathered context>"
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
- **Auto-scope**: generates commit scopes from git history automatically
- **Scope & `.gitignore` optimization**: regenerates scopes from latest history (`init --scope --force`) and re-derives `.gitignore` while preserving custom rules (`init --gitignore`)
- **Hook validation**: conventional-commit validation built in; custom hooks via shell scripts
- **Config precedence**: CLI flag > `git config --local` > `~/.config/git-agent/config.yml` > free shared-gateway default; `--free` forces the gateway and ignores the rest. Agent-session env vars (`PI_MODEL`, `CLAUDE_CODE_MODEL`, `CODEX_MODEL`) never set the inference model — it resolves only from flag / local git config / user config; the session model is read separately for `Co-Authored-By` attribution
- **Structured output**: `-o json` on every read command for scripting
