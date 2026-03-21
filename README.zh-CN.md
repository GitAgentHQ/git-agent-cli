# git-agent ![](https://img.shields.io/badge/go-1.26+-00ADFF?logo=go)

[![MIT License](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADFF?logo=go)](https://go.dev)

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

## 快速开始

```bash
# 在当前仓库初始化 git-agent
git-agent init

# 暂存变更，然后生成并创建提交
git-agent commit
```

## 命令

### `git-agent init`

在当前仓库初始化 git-agent。不带参数时，依次执行作用域生成、安装空钩子、生成 `.gitignore`。

```bash
git-agent init                          # 作用域 + 空钩子 + .gitignore（默认）
git-agent init --scope                  # 仅生成作用域
git-agent init --hook-type conventional # 安装 Conventional Commits 验证器
git-agent init --hook-type empty        # 安装空占位钩子
git-agent init --hook-script /path/to/script   # 安装自定义钩子脚本
git-agent init --gitignore              # 仅生成 .gitignore
git-agent init --force                  # 覆盖已有配置/钩子/.gitignore
git-agent init --max-commits 50         # 限制用于作用域生成的提交分析数量
```

| 参数 | 描述 |
|------|------|
| `--api-key` | AI 提供商的 API 密钥 |
| `--model` | 用于生成的模型 |
| `--base-url` | AI 提供商的 base URL |

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

### `git-agent config show`

显示解析后的 AI 提供商配置（API 密钥会被掩码）。

### `git-agent config scopes`

列出 `.git-agent/project.yml` 中定义的作用域。

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

### 项目配置（`.git-agent/project.yml`）

由 `git-agent init` 生成，定义项目的提交作用域：

```yaml
scopes:
  - api
  - core
  - auth
  - infra
hook_type: empty
```

### 钩子

通过 `git-agent init --hook-type <name>` 安装的内置钩子：

| 钩子 | 描述 |
|------|------|
| `empty` | 始终通过的占位钩子 |
| `conventional` | 验证 Conventional Commits 格式 |

自定义钩子是位于 `.git-agent/hooks/pre-commit` 的可执行脚本，通过 stdin 接收 JSON 载荷（`diff`、`commit_message`、`intent`、`staged_files`、`config`），退出 0 表示允许，非 0 表示阻止。阻止时，`git-agent` 最多重试 3 次，之后以退出码 2 结束。

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
| `--api-key` | AI 提供商的 API 密钥 |
| `--model` | 用于生成的模型 |
| `--base-url` | AI 提供商的 base URL |
| `--max-diff-lines` | 发送给模型的最大 diff 行数（默认：0，不限制） |
| `--free` | 忽略配置文件、git config 和构建时默认值；仅使用 CLI 标志或硬编码默认值 |

### 全局

| 参数 | 描述 |
|------|------|
| `-v, --verbose` | 启用详细输出 |

## 退出码

| 码 | 含义 |
|----|------|
| 0 | 成功 |
| 1 | 一般错误 — 无变更、API 失败、配置缺失 |
| 2 | 钩子阻止 — pre-commit 钩子在重试后仍返回非零 |

## 许可证

[MIT](LICENSE)
