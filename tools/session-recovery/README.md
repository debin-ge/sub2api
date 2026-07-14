# Session Recovery Tool

AI Coding Agent 会话恢复工具，支持多平台会话管理和恢复。

## 问题背景

当使用 AI Coding Agent（如 Codex、Claude Code 等）切换 API Provider（例如从 OpenAI 切换到 Anthropic）后，原有的会话记录会因为 Provider 信息不匹配而在界面中不可见。这导致用户无法查看和继续之前的对话历史。

**本工具解决的问题：**
- ✅ 自动检测已安装的 AI Coding Agent 平台
- ✅ 扫描并索引所有会话（包括隐藏的）
- ✅ 识别因 Provider 切换导致隐藏的会话
- ✅ 批量或单个恢复隐藏会话
- ✅ 恢复前自动备份，安全可靠

## 支持的平台

| 平台 | 存储方式 | 状态 |
|------|---------|------|
| Codex | JSON | ✅ 已实现 |
| Claude Code | JSONL | ✅ 已实现 |
| OpenCode | SQLite | ✅ 已实现 |
| Gemini CLI | JSON | ✅ 已实现 |
| OpenClaw | JSON | ✅ 已实现 |
| Hermes | SQLite | ✅ 已实现 |

## 安装

### 从源码编译

```bash
# 克隆仓库
git clone <repository-url>
cd tools/session-recovery

# 编译（macOS）
make build-mac

# 编译（Linux）
make build-linux

# 编译（Windows）
make build-windows

# 跨平台编译（生成所有平台版本）
make build-all
```

编译后的可执行文件位于 `bin/` 目录。

### 快速编译

```bash
go build -o bin/session-recovery main.go
```

## 使用方法

### 基本用法

```bash
# 交互式使用（推荐）
./bin/session-recovery

# 指定平台
./bin/session-recovery --platform=codex

# 查看帮助
./bin/session-recovery --help
```

### 常用命令

```bash
# 干运行模式：仅查看隐藏会话，不进行恢复
./bin/session-recovery --dry-run

# 指定平台并查看
./bin/session-recovery --platform=claude_code --dry-run

# 恢复所有隐藏会话（带确认提示）
./bin/session-recovery --restore-all

# 跳过确认提示，直接恢复所有会话
./bin/session-recovery --restore-all --yes

# 恢复特定会话（支持部分 ID 匹配）
./bin/session-recovery --session=abc123

# 组合使用
./bin/session-recovery --platform=codex --session=abc --yes
```

### 命令行参数

| 参数 | 说明 | 示例 |
|------|------|------|
| `--platform` | 指定平台名称 | `--platform=codex` |
| `--dry-run` | 仅显示会话，不恢复 | `--dry-run` |
| `--restore-all` | 恢复所有隐藏会话 | `--restore-all` |
| `--session` | 恢复特定会话（部分 ID） | `--session=abc123` |
| `--yes` | 跳过确认提示 | `--yes` |
| `-h, --help` | 显示帮助信息 | `--help` |

## 工作流程

### 1. 平台检测

工具启动时会自动检测已安装的平台：

```
🔍 正在检测已安装的 AI Coding Agent 平台...

✅ 检测到 3 个平台:
  - Codex (/Users/username/.codex/sessions)
  - Claude Code (/Users/username/.claude/projects)
  - OpenCode (/Users/username/.local/share/opencode/opencode.db)
```

### 2. 会话扫描

选择平台后，工具会扫描所有会话：

```
🔨 正在构建会话索引...
✅ 扫描完成:
   - 当前可见: 5 个会话
   - 需要恢复: 2 个会话
   - 总计: 7 个会话
```

### 3. 会话展示

对于需要恢复的会话，工具会显示详细信息：

```
📋 需要恢复的会话:

1. [session-001]
   项目: my-project
   原 Provider: OpenAI
   首条消息: 帮我实现一个用户认证功能
   消息数: 15
   最后更新: 2026-06-01 11:30:00

2. [session-003]
   项目: another-project
   原 Provider: OpenAI
   首条消息: 添加日志记录功能
   消息数: 8
   最后更新: 2026-06-15 16:00:00
```

### 4. 恢复操作

工具会在恢复前自动创建备份，确保安全：

```
💾 正在创建备份...
✅ 备份完成: /path/to/backup/sessions_20260713_143000.tar.gz

🔄 正在恢复会话...
✅ 已恢复: session-001
✅ 已恢复: session-003

✅ 恢复完成! 成功恢复 2 个会话
```

## 使用场景示例

### 场景 1：切换 API Provider 后恢复会话

**背景：**
- 之前使用 OpenAI API Key
- 切换到 Anthropic API Key
- 旧会话不可见

**解决：**

```bash
# 1. 查看隐藏的会话
./bin/session-recovery --platform=codex --dry-run

# 2. 确认需要恢复后，执行恢复
./bin/session-recovery --platform=codex --restore-all

# 3. 恢复完成，旧会话重新可见
```

### 场景 2：恢复特定项目的会话

```bash
# 查找包含特定 ID 的会话
./bin/session-recovery --session=abc123

# 确认后恢复
./bin/session-recovery --session=abc123 --yes
```

### 场景 3：批量恢复多个平台

```bash
# Codex
./bin/session-recovery --platform=codex --restore-all --yes

# Claude Code
./bin/session-recovery --platform=claude_code --restore-all --yes

# OpenCode
./bin/session-recovery --platform=opencode --restore-all --yes
```

## 工作原理

### 会话隐藏机制

AI Coding Agent 通常在会话元数据中记录 `provider` 字段（如 `OpenAI`、`Anthropic`）。当用户切换 API Key 时，工具会读取当前配置中的 provider，只显示匹配的会话。

**示例：**

```json
{
  "id": "session-001",
  "provider": "OpenAI",
  "messages": [...]
}
```

如果当前配置是 `Anthropic`，这个会话就会被隐藏。

### 恢复机制

本工具通过以下步骤恢复会话：

1. **读取当前 Provider** - 从平台配置文件中读取当前使用的 Provider
2. **扫描所有会话** - 遍历会话目录/数据库，读取所有会话元数据
3. **识别隐藏会话** - 对比会话的 `provider` 与当前 `provider`，识别不匹配的会话
4. **更新元数据** - 将隐藏会话的 `provider` 字段更新为当前值
5. **验证恢复** - 确认会话元数据已正确更新

### 数据安全

- ✅ 恢复前自动创建备份（.tar.gz 格式）
- ✅ 备份包含时间戳，便于识别
- ✅ 支持干运行模式，查看后再决定是否恢复
- ✅ 交互式确认，避免误操作
- ✅ 恢复后验证，确保成功

## 故障排查

参见 [TROUBLESHOOTING.md](./docs/TROUBLESHOOTING.md)

## 技术架构

参见 [ARCHITECTURE.md](./docs/ARCHITECTURE.md)

## 开发

```bash
# 运行测试
make test

# 代码检查
make lint

# 清理构建产物
make clean

# 查看所有命令
make help
```

## 贡献

欢迎提交 Issue 和 Pull Request。

## 许可证

MIT License

## 致谢

本工具的设计灵感来源于社区中对会话管理功能的需求。感谢所有贡献者和用户的反馈。
