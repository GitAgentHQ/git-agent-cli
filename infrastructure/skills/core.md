# Git Agent CLI

Use the bare `git-agent` command by default. It is the agent-oriented entry
point: pass the user's intent with `--intent` and let it autonomously inspect
the repository and run the commit workflow. Use subcommands only for specific
cases.

## When to use git-agent

| Situation | Command |
|---|---|
| Ready to commit changes | `git-agent --intent "..."` |
| New repo, or no scopes configured | `git-agent init` |
| Regenerate scopes from latest history | `git-agent init --scope --force` |
| Refresh / improve the `.gitignore` | `git-agent init --gitignore` |
| Provider / API key / model setup | `git-agent config show` / `config set <key> <value>` |

Use your code-search tools and targeted tests to explore the current codebase before changing a feature. `git-agent` focuses on creating reliable commits from the current diff.

## Commit workflow

1. **Intent** — derive a one-sentence intent from the conversation. If no signal exists, run `git diff --stat` to understand what changed, then form the intent from that.

2. **Commit** — run the bare agent command:
   ```
   git-agent --intent "..."
   ```
   No provider flags on the first attempt. Use `git-agent commit` only when the
   explicit commit subcommand is specifically needed.

3. **On auth error (401 / missing key)** — official release binaries already
   route through the free shared gateway with zero config. If an auth error
   still appears, retry with `--free` to force routing through the shared
   gateway, overriding any local `api_key` / `base_url` / `model`. If that still
   fails, guide the user to bring their own key via
   `~/.config/git-agent/config.yml`:
   ```yaml
   base_url: https://api.openai.com/v1
   api_key: sk-...
   model: gpt-4o
   ```
   For Cloudflare AI Gateway, use the Cloudflare AI REST API base URL and add
   `cloudflare_ai_gateway_id`. Other supported providers include local Ollama.

4. **On planner timeout** (`LLM planner timed out (model=..., after ...)`) — the
   diff was too large, or the model too slow, to plan the commit groups in time.
   `--request-timeout` is **not** a flag; raise the budget via the config key,
   then retry:
   ```
   git-agent config set request_timeout 5m
   ```
   Or shrink what the planner has to reason about: sharpen `--intent`, cap the
   diff with `--max-diff-lines <n>` / `--max-diff-bytes <n>`, or switch to a
   more capable model via `--model`.

### Structured output (`-o json`)

When you need to read the result back programmatically (which commits were
created, their SHAs, whether a hook ran), add `-o json`:

```
git-agent --intent "..." -o json
```

It prints a single object:

```json
{
  "dry_run": false,
  "commits": [
    {"title": "...", "message": "...", "files": ["..."], "sha": "...", "hook_outcome": "passed"}
  ],
  "committed_count": 1,
  "final_sha": "..."
}
```

`hook_outcome` is `passed` (a hook ran and accepted the commit) or `skipped`
(no validating hook). On `--dry-run`, `committed_count` is `0` and the `sha`
fields are empty. `commit` defaults to human-readable text; pass `-o json` only
when scripting.

## Useful flags

| Flag | When to use |
|---|---|
| `--dry-run` | User wants to preview the message without committing |
| `--no-stage` | User has already staged specific files and doesn't want auto-staging |
| `--amend` | User wants to rewrite the most recent commit message |
| `--intent "..."` | Always set — keeps generated messages focused |
| `--co-author "Name <email>"` | Attribute a co-author (repeatable) |
| `--trailer "Key: Value"` | Add an arbitrary git trailer (repeatable) |
| `--max-diff-lines N` | Cap diff size sent to the model (0 = no line limit; a byte cap always applies) |
| `--max-diff-bytes N` | Cap diff byte size sent to the model (0 or negative = built-in default ~384 KiB) |
| `-o json` | Emit machine-readable commit results (titles, SHAs, hook outcomes) for scripting; defaults to text |

`--amend` and `--no-stage` are mutually exclusive.

## Multi-commit splitting

git-agent automatically splits staged changes into multiple atomic commits (up to 5 groups) when the AI planner detects logically distinct changes. Each group is staged, committed, and hook-validated separately. No user action is needed — this is the default behavior.

## Auto-scope

If no scopes are configured for the project, git-agent generates scopes from git history automatically before planning. Each scope is a structured object with a `name` and an optional `description` (used as LLM context during commit message generation). To trigger scope generation manually: `git-agent init --scope`.

## Optimize scopes and .gitignore

Scopes and `.gitignore` are generated once at `init`, but they drift as the
project grows. Regenerate them when the layout or tech stack changes.

**Regenerate commit scopes** from the current git history and directory tree:

```
git-agent init --scope --force
```

`--force` is required when `.git-agent/config.yml` already exists — without it,
`init --scope` errors, because init guards config writes (plain `init --scope`
is meant for a fresh repo). The command re-derives the scope list from the
latest commits and layout, each scope carrying a description the planner reads
when choosing scopes, and replaces the `scopes` entry while preserving other
keys (e.g. `hook`). Verify by reading `.git-agent/config.yml`:

```yaml
scopes:
  - name: cli
    description: CLI commands and flags
  - name: docs
    description: Documentation
```

**Refresh the `.gitignore`** by re-detecting project technologies:

```
git-agent init --gitignore
```

It runs the technology detector, appends the generated rules, and preserves
anything you hand-wrote under the `### custom rules ###` section. The mandatory `.git-agent/config.local.yml` rule is re-injected idempotently. No `--force`
needed — the merge never clobbers custom rules.

## Require model co-author & Auto-Inference

Set `require_model_co_author: true` in `.git-agent/config.yml` (or user / local scope) to enforce that every commit carries a `Co-Authored-By` trailer from a known AI-provider domain or the approved name-only Ox Alpha identity.

### Automatic Model Co-Author Inference
`git-agent` derives the `Co-Authored-By` trailer solely from the active LLM session model when present — the model that actually produced the code change, read from agent-session environment variables (`PI_MODEL`, `CLAUDE_CODE_MODEL`, `CODEX_MODEL`). The model configured for `git-agent`'s own inference (`--model` flag, `git config --local git-agent.model`, or `model:` in `~/.config/git-agent/config.yml`) is strictly used for commit planning/drafting and is never attributed as a co-author.
- **Reasoning Tier & Date Suffixes** (`-high`, `-thinking`, `-non-reasoning`, `-free`, `-20241022`) are automatically stripped.
- **Model Variants** (`Flash`, `Max`, `Pro`, `Opus`, `Sonnet`) are preserved.
- **Provider mapping is not required**: a session model that maps to no known provider is still attributed — title-cased under the fallback domain. Ox Alpha is the exception: it is attributed by name only because it does not own `models.git-agent.dev`: `Co-Authored-By: Ox Alpha`.
- **Example**: `gemini-3.6-flash-high` $\rightarrow$ `Co-Authored-By: Gemini 3.6 Flash <noreply@google.com>`.

**Note for Coding Agents**: attribution and inference are separate. The generation model resolves only from the `--model` flag, `git config --local git-agent.model`, or `model:` in the user config file — in that precedence order — and is never displaced by a session-injected model. The session model (`PI_MODEL`, etc.) only supplies the `Co-Authored-By` trailer identifying who produced the change.

Built-in domains (hard-coded in source): `anthropic.com`, `openai.com`, `google.com`, `x.ai`, `zhipuai.cn`, `qwen.ai`, `deepseek.com`, `moonshot.ai`, plus the fallback domain `models.git-agent.dev` for inferred trailers of session models that map to no known provider other than Ox Alpha. Ox Alpha's name-only trailer is accepted independently of the domain list. These cannot be extended via config — edit `DefaultModelCoAuthorDomains` in code if you need custom providers.

Manual `--co-author "Name <email@domain>"` flags remain available for custom human or secondary co-author attributions, but are no longer required for active model attribution even when `require_model_co_author: true` is set.

## Hook failures

If the commit is blocked (exit code `2`), retry with a more specific
`--intent`:

```
git-agent --intent "update module path"
```

Hook exit codes (the hook script's own contract): `0` = allow, non-zero = block.

## Exit codes

`git-agent` itself uses a typed exit-code taxonomy across all commands:

| Code | Meaning |
|---|---|
| `0` | Success |
| `1` | General error (no API key, git error, no changes, etc.) |
| `2` | Commit blocked by a hook after retries |
| `3` | Retired / unused |
| `4` | Retired / unused |

## Commit format

```
<type>(<scope>): <description>

- <Bullet one>
- <Bullet two>

<Explanation paragraph>

Co-Authored-By: Git Agent <noreply@git-agent.dev>
```

- Title: lowercase, ≤50 chars, no period
- Bullets: uppercase first letter, imperative mood, ≤72 chars per bullet; LLM generates as a JSON array — trailers never enter LLM context
- Explanation: required, sentence case; lines >100 chars are wrapped to ~72 chars
- Terminal output shows only the explanation paragraph (bullets appear in the git commit body but not in the CLI output)

## Other commands

| Command | What it does |
|---|---|
| `git-agent init` | Initialize git-agent in a repo (generates scopes, .gitignore, installs hooks) |
| `git-agent init --scope [--force]` | Regenerate scopes only (`--force` required once `.git-agent/config.yml` exists) |
| `git-agent init --gitignore` | Regenerate `.gitignore` via AI (merges, preserves `### custom rules ###`) |
| `git-agent init --user --hook <value>` | Configure a hook in user-level config (`~/.config/git-agent/config.yml`), independent of any project config |
| `git-agent config show` | Show resolved provider configuration |
| `git-agent config set <key> <value>` | Set a config value (auto-selects scope) |
| `git-agent config get <key>` | Show a config value and its source scope |
| `git-agent completion <shell>` | Generate shell completions (bash/zsh/fish/powershell) |
| `git-agent skills get <name>` | Print an embedded usage document (`core`, `cli`) |
| `git-agent version` | Print build version |

## CLI reference

For the complete command reference (all flags, subcommands, config scopes,
hook types), run:

```
git-agent skills get cli
```
