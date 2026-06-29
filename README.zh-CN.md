# git-agent ![](https://img.shields.io/badge/go-1.26+-00ADFF?logo=go)

[![MIT License](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADFF?logo=go)](https://go.dev)
[![Latest Release](https://img.shields.io/github/v/release/GitAgentHQ/git-agent-cli)](https://github.com/GitAgentHQ/git-agent-cli/releases)

[English](README.md) | **简体中文**

AI 驱动的 Git 命令行工具，分析暂存和未暂存的变更，将其拆分为原子提交，并通过 LLM 生成规范的提交信息。

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

图数据库（`.git-agent/graph.db`）由 `commit`、`capture`、`timeline` 和 `graph`
命令在运行时生成。它绝不能被提交——一旦被追踪，每次运行都会再次修改它，
产生一连串 `chore: update graph database file` 提交（即"无限重建"循环）。

git-agent 自动守护这一不变量，无需 `init`：

- **`git-agent init`**：把 `.git-agent/graph.db`（及 `*.db-shm`/`*.db-wal`/`*.db-journal`、`.git-agent/config.local.yml`）写入提交版 `.gitignore`，并对已追踪的 `graph.db` 执行 `git rm --cached`，使忽略规则生效。
- **运行时防护**：每个打开图库的命令（`capture`、`timeline`、`graph *`）都会把强制忽略规则写入 `.git/info/exclude`（本地、未追踪、`git diff` 不可见），并在 `graph.db` 已被追踪时自动 untrack——例如从已提交该文件的 fork 克隆下来的仓库。即便未运行 `init`，也能阻断循环。

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
```

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

### `git-agent graph`

查询并审计 agent Event Log 及其派生的 AST + 共变索引。AST 索引解析本仓库
git 跟踪的 Go 文件，记录函数、方法、结构体/接口、**结构体字段**、类型别名、
import、调用，以及字段读取的 `references` 边。离线运行（无需 LLM、无需 API key）。

`callers` / `callees` / `node` 的**符号语法**：支持裸名、receiver 限定的
`Type.Method`，或全限定 `file::Type.Method`：

```bash
git-agent graph callers Flag                  # 任意 Flag 方法的所有调用者
git-agent graph callers decoder.alias         # 限定到某个 receiver 类型
git-agent graph callers "decode.go::decoder.alias"  # 全限定
git-agent graph callers HideHelpCommand       # 结构体字段的读取在此呈现
git-agent graph node Command.Run              # 签名 + 一跳调用链
git-agent graph affected command.go           # 覆盖该文件符号的测试
git-agent graph query --kind method Connect   # FTS5 符号搜索
```

```bash
git-agent graph status        # 索引健康度 + 行数
git-agent graph index         # 构建/刷新所有派生索引
git-agent graph sync          # 增量重放新事件到投影
git-agent graph verify        # Event Log 链完整性
git-agent graph timeline      # 操作历史（见下）
git-agent graph impact        # 共变 / structural 影响（见下）
git-agent graph callers       # 调用或引用某符号的符号
git-agent graph callees       # 某符号调用或引用的符号
git-agent graph node          # 符号的位置、签名与调用链
git-agent graph query         # FTS5 符号搜索
git-agent graph affected      # 覆盖给定文件符号的测试
git-agent graph provenance    # 文件的重命名感知变更历史
git-agent graph diagnose      # 把失败症状追溯到引入它的操作
git-agent graph external-refs # 指向外部包的调用/字段读取点
```

**外部包不索引。** 索引只解析本仓库的文件，因此来自导入包的符号（如
`github.com/spf13/pflag`）不会成为 AST 节点。`callers`/`node` 会明确提示这一
点，而不是报一个干瘪的 "not found"；`graph external-refs` 列出所有指向外部
包的调用/字段读取点：

```bash
git-agent graph callers pflag.Lookup
# Error: symbol "pflag.Lookup" is exported by external package
# "github.com/spf13/pflag", which is not indexed; run
# `git-agent graph external-refs` to list call sites into it

git-agent graph external-refs            # 所有外部包引用点
git-agent graph external-refs --json
```

> **构建说明：** AST 命令（`callers`、`callees`、`node`、`query`、
> `affected`、`impact --symbol`、`index`）需要 tree-sitter 构建
> （`CGO_ENABLED=1 go build`）。发布二进制以 `CGO_ENABLED=0` 编译并禁用这些
> 命令；`external-refs` 只读取未解析引用，两种构建下均可使用。在已有
> `.git-agent/graph.db` 的仓库升级二进制后，运行一次
> `git-agent graph index --reindex`，让旧 DB 补上结构体字段节点与
> receiver 解析后的调用边。

#### `graph` 能否帮助模型开发功能？

一次 A/B 复测（2026-06-27）在三个真实 Go 仓库（`spf13/cobra`、
`go-yaml/yaml`、`urfave/cli`）上用 capable agent 跑了对照：每个功能实现两遍，
一遍**不用** `graph`（仅 grep/Read），一遍**用** `graph` 取证命令。六组全部
build+test 通过；graph 没有把任何 fail 翻成 pass，但带来了无 graph 一侧所没有
的、可测量的非平凡价值：

- **字段消歧（cli）：** `graph query Hide` 返回空（无此字段），而
  `graph query Hidden` 返回字段节点 + `graph callers Hidden` 的 19 个读取者。
  裸 `grep Hide` 会命中三个独立字段（`Hidden`、`HideHelp`、`HideHelpCommand`）；
  graph 避免了写错字段的 accessor。
- **receiver 消歧（yaml）：** `graph node alias` 一次调用同时展示
  `parser.alias` 与 `decoder.alias` 的源码+签名，揭示 `decoder.alias` 只解引用
  已解析的 alias 节点——真正的 anchor 捕获点是 `parser.anchor`。两臂都落对了
  位置，但 graph 把 30 行噪声 grep 变成一次结构化调用。
- **测试对规格的忠实度（yaml）：** 用 graph 的一侧测试覆盖了跨多次 `Decode`
  的持久化（4 个子用例），而另一侧的单用例测试没有——尽管两边的实现完全相同。
- **跨文件消费者安全（cobra）：** `graph callers mergePersistentFlags` 暴露了
  `flag_groups.go` 与 `completions.go` 中的跨文件消费者，确认新增的只读
  accessor 不会扰动它们。

graph 的价值在于调查深度、测试/不变量忠实度、跨文件安全性——不是把不可能
变可能。在 grep 噪声大、receiver/字段消歧更关键的陌生代码库上，它的作用更明显。

### `git-agent graph impact`

查找与给定种子相关的文件或符号。三种模式：

| 模式 | 触发条件 | 返回内容 |
|------|----------|----------|
| `cochange`（默认） | 种子为文件路径（或无参数 = 工作区变更） | 历史上与种子一起变更的文件 |
| `structural` | `--symbol <name>` | 调用、被调用或引用种子符号的 AST 符号 |
| `combined` | `--symbol <name> --mode combined` | co-change 和 structural 结果的并集 |

不带参数时，种子默认为当前工作区变更。首次运行自动索引 git 历史；查询为离线操作（无需 LLM，无需 API key）。

```bash
git-agent graph impact                                     # "我的改动通常还会涉及哪些文件？"
git-agent graph impact application/commit_service.go       # 从特定文件查 co-change
git-agent graph impact src/                                # 从目录查 co-change
git-agent graph impact --symbol CommitService --json       # structural 影响分析
git-agent graph impact --symbol CommitService --mode combined  # 两种信号合并
```

| 参数 | 默认值 | 描述 |
|------|--------|------|
| `--symbol` | | 按符号名查询 structural 影响 |
| `--mode` | `cochange` | 影响模式：`cochange`、`structural` 或 `combined` |
| `--depth` | 1 | 传递性 co-change 深度 |
| `--top` | 20 | 最大结果数 |
| `--min-count` | 3 | 最小 co-change 次数阈值 |
| `--reindex` | false | 查询前强制重新索引 |
| `--json` / `--text` | 自动 | 强制输出格式（管道时为 JSON，TTY 时为文本） |

### `git-agent graph timeline`

显示最近的 agent 和人类操作历史，按会话分组，包含每次操作的工具和文件信息。由 `git-agent capture` 写入数据。离线操作。

```bash
git-agent graph timeline                        # 所有已记录的操作
git-agent graph timeline --since 2h             # 最近 2 小时
git-agent graph timeline --file src/auth.go     # 按文件过滤
git-agent graph timeline --source claude-code   # 按来源过滤
git-agent graph timeline --json                 # JSON 输出
```

| 参数 | 默认值 | 描述 |
|------|--------|------|
| `--since` | | 时间窗口：`2h`、`7d` 或 RFC 3339 时间戳 |
| `--source` | | 按操作来源过滤（如 `claude-code`、`human`） |
| `--file` | | 按文件路径过滤 |
| `--top` | 50 | 最大显示会话数 |
| `--json` / `--text` | 自动 | 强制输出格式 |

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

## 更新日志

详见 [CHANGELOG.md](CHANGELOG.md)。

## 许可证

[MIT](LICENSE)
