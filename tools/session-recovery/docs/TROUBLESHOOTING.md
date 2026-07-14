# 故障排查指南

本文档提供常见问题的解决方案。

## 目录

- [平台检测问题](#平台检测问题)
- [会话扫描问题](#会话扫描问题)
- [恢复失败问题](#恢复失败问题)
- [备份问题](#备份问题)
- [性能问题](#性能问题)

## 平台检测问题

### 问题：工具未检测到已安装的平台

**症状：**
```
🔍 正在检测已安装的 AI Coding Agent 平台...
⚠️  未检测到任何已安装的平台
```

**可能原因：**

1. **平台未安装** - 确认平台确实已安装
2. **非标准安装路径** - 平台安装在自定义目录
3. **权限问题** - 工具无法访问平台目录

**解决方案：**

```bash
# 1. 检查平台目录是否存在
ls -la ~/.codex
ls -la ~/.claude/projects
ls -la ~/.local/share/opencode

# 2. 检查目录权限
ls -ld ~/.codex
# 应该显示: drwxr-xr-x 或类似权限

# 3. 如果是权限问题，修复权限
chmod 755 ~/.codex
chmod -R 644 ~/.codex/sessions/*

# 4. 如果是自定义路径，使用环境变量指定
export CODEX_HOME=/custom/path/to/codex
./bin/session-recovery
```

### 问题：平台检测到但显示路径错误

**症状：**
```
✅ 检测到 1 个平台:
  - Codex (/Users/username/.codex/sessions)

但实际路径是 /opt/codex/data
```

**解决方案：**

编辑检测器配置或使用环境变量覆盖：

```bash
# 方法 1：使用环境变量
export CODEX_SESSION_PATH=/opt/codex/data
./bin/session-recovery --platform=codex

# 方法 2：创建符号链接
ln -s /opt/codex/data ~/.codex/sessions
```

## 会话扫描问题

### 问题：找不到任何会话

**症状：**
```
🔨 正在构建会话索引...
✅ 扫描完成:
   - 当前可见: 0 个会话
   - 需要恢复: 0 个会话
   - 总计: 0 个会话
```

**可能原因：**

1. **会话文件格式不匹配** - 工具期望的文件格式与实际不符
2. **会话目录错误** - 扫描的目录不是实际存储会话的位置
3. **文件损坏** - 会话文件损坏无法解析

**解决方案：**

```bash
# 1. 检查会话文件是否存在
# Codex
ls ~/.codex/sessions/*.json

# Claude Code
ls ~/.claude/projects/*/​*.jsonl

# OpenCode (SQLite)
sqlite3 ~/.local/share/opencode/opencode.db "SELECT COUNT(*) FROM sessions;"

# 2. 检查文件格式
# 查看 Codex 会话文件
head -20 ~/.codex/sessions/<session-id>.json

# 查看 Claude Code 会话文件
head -20 ~/.claude/projects/<project>/<session-id>.jsonl

# 3. 验证 JSON 格式
cat ~/.codex/sessions/<session-id>.json | jq .
# 如果报错，说明 JSON 格式有问题

# 4. 手动测试解析
./bin/session-recovery --platform=codex --dry-run 2>&1 | grep -i error
```

### 问题：只扫描到部分会话

**症状：**
明明有 10 个会话文件，但工具只扫描到 5 个。

**可能原因：**

1. **文件权限问题** - 部分文件无法读取
2. **文件格式错误** - 部分文件格式不符合预期
3. **解析错误** - 部分文件解析失败被跳过

**解决方案：**

```bash
# 1. 检查文件权限
ls -la ~/.codex/sessions/

# 2. 查找无法读取的文件
find ~/.codex/sessions -type f ! -readable

# 3. 修复权限
chmod 644 ~/.codex/sessions/*.json

# 4. 启用详细日志（如果工具支持）
./bin/session-recovery --platform=codex --verbose --dry-run
```

## 恢复失败问题

### 问题：恢复时报错 "Failed to update metadata"

**症状：**
```
🔄 正在恢复会话...
❌ 恢复失败: session-001
   错误: Failed to update metadata: permission denied
```

**可能原因：**

1. **文件只读** - 会话文件设置为只读
2. **磁盘空间不足** - 无法写入更新
3. **文件被占用** - 文件正在被其他进程使用

**解决方案：**

```bash
# 1. 检查文件权限
ls -la ~/.codex/sessions/<session-id>.json

# 2. 修改为可写
chmod 644 ~/.codex/sessions/<session-id>.json

# 3. 检查磁盘空间
df -h ~/.codex

# 4. 检查文件是否被占用（macOS/Linux）
lsof | grep <session-id>.json

# 5. 确保平台未运行
# 关闭 Codex/Claude Code 等应用后重试
```

### 问题：恢复后会话仍不可见

**症状：**
工具显示恢复成功，但在平台界面中仍看不到会话。

**可能原因：**

1. **缓存未刷新** - 平台缓存了旧的会话列表
2. **索引未更新** - 平台的索引数据库未更新
3. **Provider 值不正确** - 更新的 Provider 值仍与当前配置不匹配

**解决方案：**

```bash
# 1. 重启平台应用
# 完全退出并重新启动 Codex/Claude Code 等

# 2. 清除平台缓存（如果适用）
rm -rf ~/.codex/cache
rm -rf ~/.claude/cache

# 3. 验证 Provider 值已更新
cat ~/.codex/sessions/<session-id>.json | jq .provider
# 应该显示当前配置的 Provider

# 4. 检查平台当前配置
cat ~/.codex/config.json | jq .provider

# 5. 手动触发索引重建（如果平台支持）
# Codex 示例
codex --rebuild-index
```

## 备份问题

### 问题：备份创建失败

**症状：**
```
💾 正在创建备份...
❌ 备份失败: cannot create archive
```

**可能原因：**

1. **磁盘空间不足** - 没有足够空间存储备份
2. **权限问题** - 无法在备份目录创建文件
3. **路径不存在** - 备份目标目录不存在

**解决方案：**

```bash
# 1. 检查磁盘空间
df -h

# 2. 创建备份目录（如果不存在）
mkdir -p ~/.session-recovery/backups

# 3. 修改备份目录权限
chmod 755 ~/.session-recovery/backups

# 4. 手动指定备份路径（如果工具支持）
./bin/session-recovery --backup-dir=/path/to/backup --restore-all
```

### 问题：如何恢复备份

如果恢复操作导致问题，可以从备份恢复：

```bash
# 1. 找到备份文件
ls -lt ~/.session-recovery/backups/

# 2. 查看备份内容
tar -tzf ~/.session-recovery/backups/sessions_20260713_143000.tar.gz

# 3. 恢复备份
# Codex 示例
cd ~/.codex
tar -xzf ~/.session-recovery/backups/sessions_20260713_143000.tar.gz

# 4. 验证恢复
ls -la ~/.codex/sessions/
```

## 性能问题

### 问题：扫描会话非常慢

**症状：**
扫描几百个会话需要几分钟。

**可能原因：**

1. **会话文件过大** - 单个会话包含大量消息
2. **磁盘 I/O 慢** - 机械硬盘或网络存储
3. **解析效率低** - JSON 解析占用大量时间

**解决方案：**

```bash
# 1. 只扫描特定项目（如果支持）
./bin/session-recovery --platform=codex --project=my-project

# 2. 使用 SSD 存储会话数据
# 将会话目录迁移到 SSD

# 3. 定期清理旧会话
# 手动删除不需要的会话文件

# 4. 并行处理（如果工具支持）
# 查看工具是否有并发扫描选项
./bin/session-recovery --help | grep -i parallel
```

### 问题：恢复大量会话时内存不足

**症状：**
```
fatal error: out of memory
```

**解决方案：**

```bash
# 1. 分批恢复会话
# 先恢复部分会话
./bin/session-recovery --session=abc --yes
./bin/session-recovery --session=def --yes

# 2. 增加系统内存限制（Go 运行时）
GOMEMLIMIT=4GiB ./bin/session-recovery --restore-all

# 3. 重新编译并优化（开发者）
# 在代码中实现流式处理而非一次性加载所有会话
```

## SQLite 数据库问题

### 问题：SQLite 数据库被锁定

**症状：**
```
❌ 数据库错误: database is locked
```

**解决方案：**

```bash
# 1. 确保平台应用已关闭
# 完全退出 OpenCode/Hermes 等使用 SQLite 的应用

# 2. 检查是否有其他进程访问数据库
lsof | grep opencode.db

# 3. 如果数据库损坏，尝试修复
sqlite3 ~/.local/share/opencode/opencode.db "PRAGMA integrity_check;"

# 4. 最后手段：删除锁文件（谨慎使用）
rm -f ~/.local/share/opencode/opencode.db-shm
rm -f ~/.local/share/opencode/opencode.db-wal
```

## 调试技巧

### 启用详细日志

```bash
# 如果工具支持 --verbose 标志
./bin/session-recovery --verbose --dry-run

# 或使用环境变量
DEBUG=1 ./bin/session-recovery --dry-run
```

### 检查工具版本

```bash
./bin/session-recovery --version
```

### 查看配置

```bash
# 查看工具使用的配置
./bin/session-recovery --show-config
```

## 获取帮助

如果以上方案都无法解决问题，请：

1. 查看 [GitHub Issues](https://github.com/your-repo/issues)
2. 提交新的 Issue，包含：
   - 操作系统和版本
   - 工具版本
   - 平台名称和版本
   - 完整的错误信息
   - 复现步骤

## 常见错误代码

| 错误代码 | 含义 | 解决方案 |
|---------|------|---------|
| `ERR_PLATFORM_NOT_FOUND` | 未找到平台 | 检查平台是否已安装 |
| `ERR_SESSION_PARSE_FAILED` | 会话解析失败 | 检查文件格式 |
| `ERR_PERMISSION_DENIED` | 权限被拒绝 | 修改文件权限 |
| `ERR_BACKUP_FAILED` | 备份失败 | 检查磁盘空间 |
| `ERR_DB_LOCKED` | 数据库锁定 | 关闭平台应用 |
| `ERR_INVALID_PROVIDER` | 无效的 Provider | 检查配置文件 |

## 安全最佳实践

1. **始终先使用 `--dry-run`** 查看将要恢复的会话
2. **验证备份** 确保备份创建成功
3. **测试小批量** 先恢复一个会话验证
4. **关闭平台应用** 在恢复时关闭相关应用
5. **保留原始备份** 不要删除自动创建的备份文件
