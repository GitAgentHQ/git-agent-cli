# git-agent ![](https://img.shields.io/badge/go-1.26+-00ADFF?logo=go)

[![MIT License](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADFF?logo=go)](https://go.dev)

[English](README.md) | **简体中文**

使用 LLM 自动生成 Git 提交信息的 AI 优先命令行工具。

## 安装

```bash
go install github.com/fradser/git-agent@latest
```

或从 [ releases ](https://github.com/fradser/git-agent/releases) 下载预编译的二进制文件。

## 快速开始

```bash
# 在当前仓库初始化 git-agent
git agent init

# 暂存你的更改
git agent add .

# 生成并创建带有 AI 生成信息的提交
git agent commit

# 或一步完成暂存和提交
git agent commit --all
```

## 命令

### `git agent init`

在当前仓库初始化 git-agent。分析 git 历史和目录结构，生成项目特定的提交作用域。

```bash
git agent init                    # 使用默认的空钩子
git agent init --hook conventional  # 安装 Conventional Commits 验证器
git agent init --force            # 覆盖现有配置
```

### `git agent commit`

生成并创建带有 AI 生成信息的提交。

```bash
git agent commit                  # 提交暂存的更改
git agent commit --all           # 先暂存所有更改，再提交
git agent commit --dry-run       # 仅打印提交信息，不执行提交
git agent commit --intent "fix auth bug"  # 为 LLM 提供上下文提示
```

### `git agent add`

暂存文件以进行提交（`git add` 的封装）。

```bash
git agent add .
git agent add src/ utils/
```

## 配置

### 用户配置 (`~/.config/git-agent/config.yml`)

可选。指向任意 OpenAI 兼容端点：

```yaml
base_url: https://api.openai.com/v1
api_key: sk-...
model: gpt-4o
```

其他提供商的示例：

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

### 项目配置 `.git-agent/project.yml`

由 `git agent init` 生成。定义团队范围的提交类型：

```yaml
scopes:
  - api
  - core
  - auth
  - infra
```

### 钩子

内置钩子（由 `git agent init --hook <name>` 安装）：

| 钩子 | 描述 |
|------|-------------|
| `empty` | 默认空占位符，始终通过 |
| `conventional` | 验证 Conventional Commits 格式 |

自定义钩子是位于 `.git-agent/hooks/pre-commit` 的可执行脚本。它们通过 stdin 接收 JSON，退出 0 表示继续，非 0 表示阻止。

## 标志

| 标志 | 描述 |
|------|-------------|
| `-v, --verbose` | 启用详细输出 |
| `--api-key` | AI 提供商的 API 密钥 |
| `--model` | 用于生成的模型 |
| `--base-url` | AI 提供商的 base URL |
| `--max-diff-lines` | 发送给模型的最大 diff 行数（默认：500） |
| `--all, -a` | 提交前暂存所有跟踪的更改 |
| `--dry-run` | 仅打印提交信息，不执行提交 |
| `--intent` | 描述更改的意图 |
| `--co-author` | 在提交信息中添加联合作者 |

## 退出码

| 码 | 含义 |
|------|---------|
| 0 | 成功 — 提交已创建（或 dry-run 完成） |
| 1 | 一般错误 — 无暂存更改、API 失败、配置缺失 |
| 2 | 钩子阻止 — pre-commit 钩子返回非零 |

## 许可证

[MIT](LICENSE)
