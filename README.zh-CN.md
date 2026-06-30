# git-agent ![](https://img.shields.io/badge/go-1.26+-00ADFF?logo=go)

[![MIT License](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADFF?logo=go)](https://go.dev)
[![Latest Release](https://img.shields.io/github/v/release/GitAgentHQ/git-agent-cli)](https://github.com/GitAgentHQ/git-agent-cli/releases)

[English](README.md) | **简体中文**

面向 agent 的 Git 命令行工具：原子化 AI 提交 + 共变关系——全语言、离线、无需 API key。它分析暂存和未暂存的变更，将其拆分为原子提交并通过 LLM 生成规范的提交信息；共变查询则挖掘 git 历史，找出习惯性一起变更的文件，并附上解释这种耦合的提交信息。

## 安装

**Homebrew（macOS/Linux）：**

```bash
brew install GitAgentHQ/brew/git-agent
```

**Go install：**

```bash
go install github.com/gitagenthq/git-agent@latest
```

**预编译二进制文件：** 从 [releases 页面](https://github.com/GitAgentHQ/git-agent-cli/releases) 下载。

### Agent skill

安装 git-agent skill，让 AI agent 代替你执行提交：

```bash
npx skills add https://github.com/GitAgentHQ/git-agent-cli --skill using-git-agent
```

## 快速开始

```bash
# 在当前仓库初始化 git-agent
git-agent init

# 暂存变更，然后生成并创建提交
git-agent commit
```

## 命令

### `git-agent init`

在当前仓库初始化 git-agent。不带参数时，运行完整向导：生成 `.gitignore`、从 git 历史生成提交作用域，并写入 `.git-agent/config.yml`（包含作用域和 `hook: [conventional]`）。

```bash
git-agent init                          # 完整向导（gitignore + 作用域 + conventional 钩子）
git-agent init --scope                  # 仅生成作用域
git-agent init --gitignore              # 仅生成 .gitignore
git-agent init --hook conventional      # 安装 Conventional Commits 验证器
git-agent init --hook empty             # 安装空占位钩子
git-agent init --hook /path/to/script   # 安装自定义钩子脚本
git-agent init --agent-hook             # 安装 Claude Code PostToolUse 捕获钩子
git-agent init --force                  # 覆盖已有配置/钩子/.gitignore
git-agent init --max-commits 50         # 限制用于作用域生成的提交分析数量
git-agent init --local --scope          # 将作用域写入 .git-agent/config.local.yml
```

| 参数 | 描述 |
|------|------|
| `--scope` | 通过 AI 生成作用域 |
| `--gitignore` | 通过 AI 生成 `.gitignore` |
| `--hook` | 配置钩子：`conventional`、`empty` 或文件路径（可重复） |
| `--agent-hook` | 安装 Claude Code PostToolUse 捕获钩子 |
| `--force` | 覆盖已有配置/.gitignore |
| `--max-commits` | 用于作用域生成的最大提交分析数量（默认：200） |
| `--local` | 将配置写入 `.git-agent/config.local.yml`（需要至少一个操作参数） |
| `--user` | 将配置写入 `~/.config/git-agent/config.yml`（需要至少一个操作参数） |

#### `.git-agent/graph.db` 永不追踪

图数据库（`.git-agent/graph.db`）由 `commit`、`capture`、`timeline`、`related`、
`status` 等命令在运行时生成。它绝不能被提交——一旦被追踪，每次运行都会再次修改它，
产生一连串 `chore: update graph database file` 提交（即"无限重建"循环）。

git-agent 自动守护这一不变量，无需 `init`：

- **`git-agent init`**：把 `.git-agent/graph.db`（及 `*.db-shm`/`*.db-wal`/`*.db-journal`、`.git-agent/config.local.yml`）写入提交版 `.gitignore`，并对已追踪的 `graph.db` 执行 `git rm --cached`，使忽略规则生效。
- **运行时防护**：每个打开图库的命令（`capture`、`timeline`、`related`、`status`）都会把强制忽略规则写入 `.git/info/exclude`（本地、未追踪、`git diff` 不可见），并在 `graph.db` 已被追踪时自动 untrack——例如从已提交该文件的 fork 克隆下来的仓库。即便未运行 `init`，也能阻断循环。

存疑时验证：

```bash
git ls-files .git-agent/graph.db        # 应无输出（未追踪）
git check-ignore .git-agent/graph.db    # 输出路径，exit 0（已忽略）
```

### `git-agent commit`

读取暂存和未暂存的变更，将其拆分为原子组，为每组生成提交信息，并依次提交。

```bash
git-agent commit                              # 提交所有变更
git-agent commit --dry-run                    # 仅打印提交信息，不执行提交
git-agent commit --no-stage                   # 仅提交已暂存的变更
git-agent commit --amend                      # 重新生成并修改最后一次提交
git-agent commit --intent "fix auth bug"      # 向 LLM 提供上下文提示
git-agent commit --co-author "Name <email>"  # 添加 co-author trailer
git-agent commit --trailer "Fixes: #123"     # 添加任意 git trailer
git-agent commit --no-attribution             # 省略默认的 Git Agent trailer
git-agent commit -o json                      # 结构化结果（标题、SHA、钩子结果）
```

使用 `-o json` 时，commit 打印单个对象：`dry_run`、`commits[]`（每项
`{title, message, files, sha, hook_outcome}`）、`committed_count` 和
`final_sha`。`hook_outcome` 为 `passed` 或 `skipped`。否则输出为人类可读文本。

### `git-agent config`

管理 git-agent 配置。

```bash
git-agent config show              # 显示解析后的提供商配置（API 密钥已脱敏）
git-agent config get <key>         # 显示某配置项的解析值及来源作用域
git-agent config set <key> <value> # 将配置值写入对应作用域
git-agent config set --user api-key sk-xxx   # 写入用户作用域
git-agent config set --project hook empty     # 写入项目作用域
git-agent config set --local max-diff-lines 1000  # 写入本地作用域
git-agent config set --local max-diff-bytes 524288 # 提高字节上限（如直连端点放宽到 512 KiB）
```

`config set` 和 `config get` 同时支持 snake_case 和 kebab-case 键名（如 `api-key` 和 `api_key` 等价）。

| 作用域参数 | 配置文件 | 用途 |
|------------|----------|------|
| `--user` | `~/.config/git-agent/config.yml` | 提供商密钥（api_key、base_url、model） |
| `--project` | `.git-agent/config.yml` | 共享配置，提交到 git |
| `--local` | `.git-agent/config.local.yml` | 本地覆盖，gitignore |

未指定作用域参数时，提供商密钥默认写入 `--user`，其他配置项默认写入 `--project`。

### `git-agent completion`

生成 shell 自动补全脚本。

```bash
git-agent completion bash         # bash 补全
git-agent completion zsh          # zsh 补全
git-agent completion fish         # fish 补全
git-agent completion powershell   # PowerShell 补全
```

持久化加载（运行一次即可）：

```bash
# bash (macOS)
git-agent completion bash > $(brew --prefix)/etc/bash_completion.d/git-agent

# zsh
git-agent completion zsh > "${fpath[1]}/_git-agent"

# fish
git-agent completion fish > ~/.config/fish/completions/git-agent.fish
```

### `git-agent version`

打印构建版本。

### `git-agent related`

挖掘 git 历史，找出历史上与给定文件一起变更的文件（共变耦合）。种子可以是
文件路径、目录，或不带参数时取当前工作区的变更（"我的改动通常还会涉及哪些
文件？"）。与多个种子都耦合的文件排名最高。

它**全语言**——只读取 git 历史，不解析源码——离线运行，无需 API key，首次
运行自动索引。对 agent Event Log 的取证查询见
[`git-agent audit`](#git-agent-audit)。

使用 `-o json` 时，每个相关文件都附带一个 `commits` 数组（每项
`{sha, subject, ts}`）——把这些文件联系起来的提交，即"它们为什么相关？"的证据。

```bash
git-agent related                                     # "我的改动通常还会涉及哪些文件？"
git-agent related application/commit_service.go       # 从特定文件查共变
git-agent related src/                                # 从目录查共变
git-agent related --tests                             # 只保留相关的测试文件（"该跑哪些测试？"）
git-agent related application/commit_service.go -o json
```

| 参数 | 默认值 | 描述 |
|------|--------|------|
| `--depth` | 1 | 传递性共变深度 |
| `--top` | 20 | 最大结果数 |
| `--min-count` | 3 | 最小共变次数阈值 |
| `--tests` | false | 只保留相关的测试文件（决定改动后该跑哪些测试） |
| `--reindex` | false | 查询前强制重新索引 |
| `-o`、`--output` | 自动 | 输出格式：`auto`、`json`、`text`（管道时为 JSON，TTY 时为文本） |

### `git-agent status`

显示图索引的健康度与行数：提交、文件、作者、共变对、会话、操作、最后索引的
提交，以及数据库大小。读取前自动同步投影。离线操作（无需 LLM，无需 API key）。

```bash
git-agent status            # 索引健康度 + 行数
git-agent status -o json    # JSON 输出
git-agent init --graph      # 一次性全量建图（共变 + Event-Log 投影）
```

### `git-agent audit`

查询并审计 agent Event Log——每次捕获的 agent 与人类操作的追加式、哈希链记录。
所有查询均为只读、离线（无需 LLM，无需 API key）。

```bash
git-agent audit timeline      # agent/人类操作历史（从这里开始）
git-agent audit diagnose      # 把回归追溯到引入它的操作
git-agent audit provenance    # 文件的重命名感知变更历史
git-agent audit verify        # Event Log 链完整性（断裂时退出码 4）
```

#### `git-agent audit timeline`

显示最近的 agent 和人类操作历史，按会话分组，包含每次操作的工具和文件信息。由 `git-agent capture` 写入数据。离线操作。

```bash
git-agent audit timeline                        # 所有已记录的操作
git-agent audit timeline --since 2h             # 最近 2 小时
git-agent audit timeline --file src/auth.go     # 按文件过滤
git-agent audit timeline --source claude-code   # 按来源过滤
git-agent audit timeline -o json                # JSON 输出
```

| 参数 | 默认值 | 描述 |
|------|--------|------|
| `--since` | | 时间窗口：`2h`、`7d` 或 RFC 3339 时间戳 |
| `--source` | | 按操作来源过滤（如 `claude-code`、`human`） |
| `--file` | | 按文件路径过滤 |
| `--top` | 50 | 最大显示会话数 |
| `-o`、`--output` | 自动 | 输出格式：`auto`、`json`、`text` |

#### `git-agent audit diagnose`

把回归追溯到最可能引入它的 agent 操作：在最后一次通过与首次失败的测试结果之间推导出嫌疑窗口，用共变扩展相关文件集，并对嫌疑操作排序。`--file <source>` 为相关集提供种子（产出候选时实际必需）；`[symptom]` 为可选上下文；`--llm` 用配置的 diagnose LLM 对靠前候选重排序。除非 `--force`，Event Log 链断裂时退出码 4。

#### `git-agent audit provenance`

从 Event Log 重建文件完整的、重命名感知的变更历史：每次捕获的变更加上带外编辑，并折叠其重命名前的身份。带外行会被标记。用法：`git-agent audit provenance <file>`。

#### `git-agent audit verify`

遍历哈希链的 Event Log 并验证其未被篡改（重算每个事件的哈希，检查链接与序号连续性）。任何完整性断裂都会退出码 4。

## 配置

### 用户配置（`~/.config/git-agent/config.yml`）

可选。指向任意 OpenAI 兼容端点：

```yaml
base_url: https://api.openai.com/v1
api_key: sk-...
model: gpt-4o
```

其他提供商示例：

```yaml
# Cloudflare Workers AI
base_url: https://api.cloudflare.com/client/v4/accounts/YOUR_ACCOUNT_ID/ai/v1
api_key: YOUR_CLOUDFLARE_API_TOKEN
model: "@cf/meta/llama-3.1-8b-instruct"
```

```yaml
# 本地 Ollama
base_url: http://localhost:11434/v1
model: llama3
```

### 项目配置（`.git-agent/config.yml`）

由 `git-agent init` 生成，定义项目的提交作用域和钩子配置。同时为了向后兼容，也读取 `.git-agent/project.yml`：

```yaml
scopes:
  - api
  - core
  - auth
  - infra
hook:
  - conventional
```

### 钩子

通过 `init --hook` 配置，或之后使用 `git-agent config set hook <value>` 更新：

| 钩子 | 描述 |
|------|------|
| `conventional` | 验证 Conventional Commits 格式（Go 原生实现） |
| `empty` | 始终通过的占位钩子 |
| `<文件路径>` | Go 验证 + 指定路径的 shell 脚本 |

自定义钩子通过 stdin 接收 JSON 载荷（`diff`、`commitMessage`、`intent`、`stagedFiles`、`config`），退出 0 表示允许，非 0 表示阻止。阻止时，`git-agent` 最多重试 3 次，之后以退出码 2 结束。

## 参数

### `commit`

| 参数 | 描述 |
|------|------|
| `--dry-run` | 仅打印提交信息，不执行提交 |
| `--no-stage` | 跳过自动暂存，仅提交已暂存的变更 |
| `--amend` | 重新生成并修改最后一次提交（无规划或钩子） |
| `--intent` | 描述本次变更的意图 |
| `--co-author` | 添加 co-author trailer（可重复） |
| `--trailer` | 添加任意 git trailer，格式为 `Key: Value`（可重复） |
| `--no-attribution` | 省略默认的 Git Agent co-author trailer |
| `--max-diff-lines` | 发送给模型的最大 diff 行数（默认：0，不限制行数；字节上限始终生效） |
| `--max-diff-bytes` | 发送给模型的最大 diff 字节数（默认：0，使用内置约 384 KiB 上限；传正值可覆盖） |
| `-o`、`--output` | 输出格式：`text`（默认）、`json` 或 `auto`（管道时为 JSON） |

### 全局

| 参数 | 描述 |
|------|------|
| `--api-key` | AI 提供商的 API 密钥 |
| `--model` | 用于生成的模型 |
| `--base-url` | AI 提供商的 base URL |
| `-v, --verbose` | 启用详细输出 |
| `--free` | 仅使用构建时内嵌凭证；忽略配置文件和 git config |

## 退出码

| 码 | 含义 |
|----|------|
| 0 | 成功 |
| 1 | 一般错误 — 无变更、API 失败、配置缺失 |
| 2 | 钩子阻止 — pre-commit 钩子在重试后仍返回非零 |
| 3 | 已弃用（未使用）— 旧版用于读取前图未索引的情形；共变读取会在首次运行时自动索引，此码不再使用 |
| 4 | Event Log 链完整性断裂（`audit verify` / `audit diagnose`） |

## 更新日志

详见 [CHANGELOG.md](CHANGELOG.md)。

## 许可证

[MIT](LICENSE)
