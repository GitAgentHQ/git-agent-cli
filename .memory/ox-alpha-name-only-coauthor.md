---
name: ox-alpha-name-only-coauthor
description: Ox Alpha must be attributed by name without a synthetic email domain
type: decision
---

# Ox Alpha uses a name-only co-author trailer

Ox Alpha does not own `models.git-agent.dev`. When the active session model is
Ox Alpha, git-agent emits `Co-Authored-By: Ox Alpha` without an email. The
model co-author validator accepts that exact identity case-insensitively, while
other unmapped models continue using the fallback domain.

## Why

A synthetic `noreply@models.git-agent.dev` address misrepresents Ox Alpha's
identity. Git trailers are free-form, so a name-only co-author is valid.

## How to apply

Keep the Ox Alpha special case in model inference and model co-author
validation. Do not add an Ox Alpha email address or treat every name-only
co-author as a valid model attribution.

## Related

[[commit-flow-gathers-context-first]]
