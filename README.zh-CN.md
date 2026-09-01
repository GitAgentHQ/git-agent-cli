# git-agent ![](https://img.shields.io/badge/go-1.26+-00ADFF?logo=go)

[![MIT License](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADFF?logo=go)](https://go.dev)
[![Latest Release](https://img.shields.io/github/v/release/GitAgentHQ/git-agent-cli)](https://github.com/GitAgentHQ/git-agent-cli/releases)

[English](README.md) | **简体中文**

面向 agent 的 Git 命令行工具：它分析暂存和未暂存的变更，将其拆分为原子提交，并通过 LLM 生成规范的提交信息。

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
git-agent init --force                  # 覆盖已有配置/钩子/.gitignore
git-agent init --max-commits 50         # 限制用于作用域生成的提交分析数量
git-agent init --local --scope          # 将作用域写入 .git-agent/config.local.yml
```

| 参数 | 描述 |
|------|------|
| `--scope` | 通过 AI 生成作用域 |
| `--gitignore` | 通过 AI 生成 `.gitignore` |
| `--hook` | 配置钩子：`conventional`、`empty` 或文件路径（可重复） |
| `--force` | 覆盖已有配置/.gitignore |
| `--max-commits` | 用于作用域生成的最大提交分析数量（默认：200） |
| `--local` | 将配置写入 `.git-agent/config.local.yml`（需要至少一个操作参数） |
| `--user` | 将配置写入 `~/.config/git-agent/config.yml`（需要至少一个操作参数） |

### `git-agent`（裸命令）

面向 agent 的默认入口。它会检查仓库，执行常规的自动检查，将变更拆分为原子组，生成提交信息并依次提交。

```bash
git-agent --intent "fix auth bug"           # 默认 agent 工作流
```

### `git-agent commit`

当不需要 autonomous root 工作流时使用的显式提交子命令。它读取暂存和未暂存的变更，将其拆分为原子组，为每组生成提交信息，并依次提交。

```bash
git-agent commit                              # 显式提交路径
git-agent commit --dry-run                    # 仅打印提交信息，不执行提交
git-agent commit --no-stage                   # 仅提交已暂存的变更
git-agent commit --amend                      # 重新生成并修改最后一次提交
git-agent commit --intent "fix auth bug"      # 向 LLM 提供上下文提示
git-agent commit --co-author "Name <email>"  # 添加 co-author trailer
git-agent commit --trailer "Fixes: #123"     # 添加任意 git trailer
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
git-agent config set language auto             # 跟随 --intent 语言，否则使用英文
git-agent config set --local language Japanese # 在本地覆盖语言
git-agent config set --local max-diff-lines 1000  # 写入本地作用域
git-agent config set --local max-diff-bytes 524288 # 提高字节上限（如直连端点放宽到 512 KiB）
git-agent config set --local max-plan-files 300     # 提高规划阶段文件列表上限（超出后按目录折叠）
```

`config set` 和 `config get` 同时支持 snake_case 和 kebab-case 键名（如 `api-key` 和 `api_key` 等价）。

| 作用域参数 | 配置文件 | 用途 |
|------------|----------|------|
| `--user` | `~/.config/git-agent/config.yml` | 提供商密钥和 Cloudflare AI Gateway ID |
| `--project` | `.git-agent/config.yml` | 共享配置，提交到 git |
| `--local` | `.git-agent/config.local.yml` | 本地覆盖，gitignore |

未指定作用域参数时，提供商密钥默认写入 `--user`，其他配置项默认写入 `--project`。

生成的提交信息默认使用 `language: auto`：标题描述、要点和说明会跟随清晰的
`--intent` 指令语言；如果无法识别指令语言，则使用英文。也可以显式设置语言，
例如 `git-agent config set language Japanese`。本地配置优先于项目配置，项目配置
优先于用户配置。Conventional Commit 的类型和作用域保持标准语法，只翻译自然语言文本。

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

## 配置

### 用户配置（`~/.config/git-agent/config.yml`）

可选。指向任意 OpenAI 兼容端点：

```yaml
base_url: https://api.openai.com/v1
api_key: sk-...
model: gpt-4o
```

### 免费共享网关（零配置）

官方 release 二进制默认指向免费共享网关，因此**无需任何配置**——直接运行
`git-agent commit` 即可。请求经由一个 Cloudflare Worker 转发，上游凭据由
服务端持有（绝不进入二进制），并对匿名免费用户按 IP 限流。

若想使用自己的端点，设置 `base_url`（可选 `api_key` / `model`）即可——
任何用户配置都会覆盖内置网关 URL。

其他提供商示例：

```yaml
# 自带 key —— Cloudflare AI Gateway + Workers AI
base_url: https://api.cloudflare.com/client/v4/accounts/YOUR_ACCOUNT_ID/ai/v1
api_key: YOUR_CLOUDFLARE_API_TOKEN
model: "@cf/zai-org/glm-4.7-flash"
cloudflare_ai_gateway_id: YOUR_GATEWAY_ID # 默认 Gateway 可填写 "default"
```

自带 key 时，git-agent 直接路由到你配置的端点；对 Cloudflare 设置
`cloudflare_ai_gateway_id` 可接入 Gateway，保留用量元数据但不保存 prompt
和 response 内容，并由 CLI 独占重试策略，避免多层重试相乘。

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
language: auto # 或显式指定语言，例如 Japanese
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
| `--max-diff-lines` | 发送给模型的最大 diff 行数（默认：0，不限制行数；字节上限始终生效） |
| `--max-diff-bytes` | 发送给模型的最大 diff 字节数（默认：0，使用内置约 384 KiB 上限；传正值可覆盖） |
| `--max-plan-files` | 规划提示中单独列出的最大文件路径数，超出后按目录折叠（默认：0，使用内置上限 150） |
| `-o`、`--output` | 输出格式：`text`（默认）、`json` 或 `auto`（管道时为 JSON） |

### 全局

| 参数 | 描述 |
|------|------|
| `--api-key` | AI 提供商的 API 密钥 |
| `--model` | 用于生成的模型 |
| `--base-url` | AI 提供商的 base URL |
| `-v, --verbose` | 启用详细输出 |

## 退出码

| 码 | 含义 |
|----|------|
| 0 | 成功 |
| 1 | 一般错误 — 无变更、API 失败、配置缺失 |
| 2 | 钩子阻止 — pre-commit 钩子在重试后仍返回非零 |
| 3 | 已弃用（未使用）— 旧版用于读取前图未索引的情形；共变读取会在首次运行时自动索引，此码不再使用 |
| 4 | 已弃用（未使用）— 旧版用于 Event Log 链完整性；Event Log 子系统已移除，此码不再使用 |

## 更新日志

详见 [CHANGELOG.md](CHANGELOG.md)。

## 许可证

[MIT](LICENSE)
