# GitAgent V1 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Load `superpowers:executing-plans` skill using the Skill tool to implement this plan task-by-task.

**Goal:** Build `ga` CLI - an AI-first Git CLI tool for automated commit message generation using OpenAI-compatible LLMs with hook-based policy enforcement.

**Architecture:** Go-based CLI using cobra for command structure, Clean Architecture with domain/application/infrastructure layers, embedded hook templates via go:embed.

**Tech Stack:** Go 1.21+, cobra, go-openai, go:embed for hooks

**Design Support:**
- [BDD Specs](../2026-03-18-gitagent-v1-design/bdd-specs.md)
- [Architecture](../2026-03-18-gitagent-v1-design/architecture.md)

## Execution Plan

```yaml
tasks:
  - id: "001"
    subject: "Setup project structure and dependencies"
    slug: "setup-project-structure"
    type: "setup"
    depends-on: []
  - id: "002"
    subject: "Define domain layer interfaces"
    slug: "define-domain-interfaces"
    type: "impl"
    depends-on: ["001"]
  - id: "003"
    subject: "Config resolver test"
    slug: "config-resolver-test"
    type: "test"
    depends-on: ["002"]
  - id: "004"
    subject: "Config resolver implementation"
    slug: "config-resolver-impl"
    type: "impl"
    depends-on: ["003"]
  - id: "005"
    subject: "ga init command test"
    slug: "ga-init-test"
    type: "test"
    depends-on: ["004"]
  - id: "006"
    subject: "ga init command implementation"
    slug: "ga-init-impl"
    type: "impl"
    depends-on: ["005"]
  - id: "007"
    subject: "ga commit core flow test"
    slug: "ga-commit-core-test"
    type: "test"
    depends-on: ["004"]
  - id: "008"
    subject: "ga commit core flow implementation"
    slug: "ga-commit-core-impl"
    type: "impl"
    depends-on: ["007"]
  - id: "009"
    subject: "Diff filter test"
    slug: "diff-filter-test"
    type: "test"
    depends-on: ["002"]
  - id: "010"
    subject: "Diff filter implementation"
    slug: "diff-filter-impl"
    type: "impl"
    depends-on: ["009"]
  - id: "011"
    subject: "Hook executor test"
    slug: "hook-executor-test"
    type: "test"
    depends-on: ["002"]
  - id: "012"
    subject: "Hook executor implementation"
    slug: "hook-executor-impl"
    type: "impl"
    depends-on: ["011"]
  - id: "013"
    subject: "Verbose mode and output contract test"
    slug: "verbose-output-test"
    type: "test"
    depends-on: ["008"]
  - id: "014"
    subject: "Verbose mode and output contract implementation"
    slug: "verbose-output-impl"
    type: "impl"
    depends-on: ["013"]
  - id: "015"
    subject: "Error handling and exit codes test"
    slug: "error-handling-test"
    type: "test"
    depends-on: ["008", "010", "012"]
  - id: "016"
    subject: "Integration tests"
    slug: "integration-tests"
    type: "test"
    depends-on: ["006", "008", "010", "012", "014"]
```

**Task File References (for detailed BDD scenarios):**
- [Task 001: Setup project structure and dependencies](./task-001-setup-project-structure.md)
- [Task 002: Define domain layer interfaces](./task-002-define-domain-interfaces.md)
- [Task 003: Config resolver test](./task-003-config-resolver-test.md)
- [Task 004: Config resolver implementation](./task-004-config-resolver-impl.md)
- [Task 005: ga init command test](./task-005-ga-init-test.md)
- [Task 006: ga init command implementation](./task-006-ga-init-impl.md)
- [Task 007: ga commit core flow test](./task-007-ga-commit-core-test.md)
- [Task 008: ga commit core flow implementation](./task-008-ga-commit-core-impl.md)
- [Task 009: Diff filter test](./task-009-diff-filter-test.md)
- [Task 010: Diff filter implementation](./task-010-diff-filter-impl.md)
- [Task 011: Hook executor test](./task-011-hook-executor-test.md)
- [Task 012: Hook executor implementation](./task-012-hook-executor-impl.md)
- [Task 013: Verbose mode and output contract test](./task-013-verbose-output-test.md)
- [Task 014: Verbose mode and output contract implementation](./task-014-verbose-output-impl.md)
- [Task 015: Error handling and exit codes test](./task-015-error-handling-test.md)
- [Task 016: Integration tests](./task-016-integration-tests.md)

## BDD Coverage

All BDD scenarios from the design are covered by these tasks:

| Feature | Scenarios Covered | Task IDs |
|---------|------------------|----------|
| ga init - Happy Path | Init with default empty hook, Init with built-in conventional hook, Unknown hook name, Fresh repo, Custom max-commits | 005, 006 |
| ga init - Error Scenarios | Config exists, Hook exists, --force, Not git repo, Missing API key | 005, 006, 015 |
| ga commit - Happy Path | Generate commit, Scopes from config, Co-Author-By, User intent, Dry-run | 007, 008 |
| ga commit - Diff Filtering | Lock files excluded, Binary excluded, Truncation | 009, 010 |
| ga commit - Config Resolution | API key precedence, Custom model, Custom base URL | 003, 004 |
| ga commit - Error Scenarios | No staged changes, Missing API key, LLM errors | 015 |
| Hook System | Hook passes, Hook blocks, No hook, Not executable | 011, 012 |
| Verbose Mode | Verbose output, Truncation info | 013, 014 |
| Exit Codes | Success (0), Error (1), Hook block (2) | 015 |
| Output Contract | stdout/stderr isolation | 013, 014 |

## Dependency Chain

```
task-001 (setup)
    │
    ├─→ task-002 (domain-interfaces)
    │        │
    │        ├─→ task-003 (config-test) ──→ task-004 (config-impl)
    │        │           │
    │        │           └─→ task-005 (init-test) ──→ task-006 (init-impl)
    │        │
    │        ├─→ task-009 (diff-filter-test) ──→ task-010 (diff-filter-impl)
    │        │
    │        └─→ task-011 (hook-test) ──→ task-012 (hook-impl)
    │
    └─→ task-004 (config-impl)
              │
              ├─→ task-007 (commit-core-test) ──→ task-008 (commit-core-impl)
              │           │
              │           └─→ task-013 (verbose-test) ──→ task-014 (verbose-impl)
              │                       │
              └─→ task-015 (error-handling-test)
                              │
                              └─→ task-016 (integration-tests)
```

**Analysis**:
- No circular dependencies
- Logical dependency flow: setup → domain → config → features → integration
- Tests (Red) precede implementations (Green) for each feature
- Integration tests depend on all feature implementations

---

## Execution Handoff

Plan complete and saved to `docs/plans/2026-03-18-gitagent-v1-plan/`. **Execution options:**

**1. Orchestrated Execution (Recommended)** - Load `superpowers:executing-plans` skill using the Skill tool.

**2. Direct Agent Team** - Load `superpowers:agent-team-driven-development` skill using the Skill tool.

**3. BDD-Focused Execution** - Load `superpowers:behavior-driven-development` skill using the Skill tool for specific scenarios.
