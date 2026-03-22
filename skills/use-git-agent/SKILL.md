---
name: use-git-agent
description: Use git-agent to commit changes with AI-generated conventional commit messages. Immediately runs git-agent commit when loaded — no setup or configuration questions unless an error occurs.
---

# Git Agent Commit

When this skill is loaded, **immediately** run `git-agent commit`. Do not ask the user what to do. Do not show a menu.

## Steps

1. **Intent** — derive a one-sentence intent from the conversation. If no signal exists, run `git diff --stat` to understand what changed, then form the intent from that.

2. **Commit** — run:
   ```
   git-agent commit --intent "..."
   ```
   No provider flags on the first attempt.

3. **On auth error (401 / missing key)** — retry once with `--free`:
   ```
   git-agent commit --intent "..." --free
   ```

4. **If `--free` also fails** — guide the user to create `~/.config/git-agent/config.yml`:
   ```yaml
   base_url: https://api.openai.com/v1
   api_key: sk-...
   model: gpt-4o
   ```
   Other supported providers: Cloudflare Workers AI, local Ollama.

## Useful flags

| Flag | When to use |
|---|---|
| `--dry-run` | User wants to preview the message without committing |
| `--no-stage` | User has already staged specific files and doesn't want auto-staging |
| `--amend` | User wants to rewrite the most recent commit message |
| `--intent "..."` | Always set — keeps generated messages focused |

`--amend` and `--no-stage` are mutually exclusive.

## Hook failures

If the commit is blocked (exit code `2`), retry with a more specific `--intent`:

```
git-agent commit --intent "update module path"
```

Hook exit codes: `0` = allow, non-zero = block. After 3 retries per group and 2 re-plans, `git-agent` exits with code `2`.

## Commit format

```
<type>(<scope>): <description>

- <Action> <component> <detail>

<explanation paragraph>

Co-Authored-By: Git Agent
```

- Title: lowercase, ≤50 chars, no period
- Body lines: ≤72 chars, `- ` prefix, imperative verbs
- Explanation paragraph: required

## CLI reference

Full command reference (all flags, subcommands, config scopes, hook types): [references/cli.md](references/cli.md)
