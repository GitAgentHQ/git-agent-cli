---
name: skill-stub-symlink-to-repo
description: The installed using-git-agent skill is a symlink chain into git-agent-cli/skills/, so the repo copy is the single source of truth
type: decision
---

# using-git-agent skill stub lives in this repo

`~/.pi/agent/skills/using-git-agent` -> `~/.agents/skills/using-git-agent`
(symlink) -> `/Users/FradSer/Developer/FradSer/git-agent/git-agent-cli/skills/using-git-agent/SKILL.md`.
There is exactly one file; editing any path on the chain edits the canonical
repo copy.

**Why**: installs (`npx skills add ... --skill using-git-agent`) distribute
from this repo, and the local agent install is linked rather than copied.
Editing only an "installed" path already changes what ships with the next
release — no sync step exists or is needed.

**How to apply**: when changing skill-stub behavior, edit
`skills/using-git-agent/SKILL.md` in this repo directly (or through either
symlink) and commit it here; never "fix" the home-directory copy separately.
The detailed workflow guides are separate: they are embedded in the binary
(`infrastructure/skills/core.md`, `cli.md`) and served at runtime via
`git-agent skills get`.

Related: [[commit-flow-gathers-context-first]]
