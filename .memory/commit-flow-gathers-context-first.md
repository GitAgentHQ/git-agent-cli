---
name: commit-flow-gathers-context-first
description: using-git-agent stub explicitly encodes gather-context-then-intent as a mandatory two-step commit flow
type: decision
---

# Commit flow: gather context first, then assemble --intent

The user asked (2026-02) that `skills/using-git-agent/SKILL.md` state
explicitly that every `git-agent` commit is two steps:

1. Gather session context (`session_context` tool in pi; fall back to reading
   the diff where no such tool exists).
2. Condense it into one concise sentence and pass it via
   `git-agent --intent "<sentence>"`.

Never build the intent from memory alone, never paste raw diffs or file lists.

**Why**: agents were passing generic or invented intents, losing the user's
actual request/decisions from the session; the harness-level rule
("session_context before committing") needed to live inside the skill so any
agent loading it follows the same flow.

**How to apply**: keep this wording when editing the stub; the pi-git-agent
package (`../pi-git-agent/`) is kept consistent with it — its guard
(`extensions/validate-commit.ts`), procedures, and injected GUIDANCE all
require bare `git-agent --intent` too (never the menu as redirect target,
never `git-agent commit` as primary). The session-context commit-boundary
check accepts both bare and subcommand forms. The served `core.md` has no
commit section yet; if it gains one, keep all three surfaces aligned. The
stub is the repo file — see [[skill-stub-symlink-to-repo]].

Related: [[skill-stub-symlink-to-repo]]
