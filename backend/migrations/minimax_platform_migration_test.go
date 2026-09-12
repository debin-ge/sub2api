package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMiniMaxPlatformMigration(t *testing.T) {
	content, err := FS.ReadFile("237_add_minimax_platform.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	// 'bytedance' 不是笔误，别"修"回去：237 按文件名排在 267/268 之前，但在 test 血脉的库上
	// 是 main → test 合并后才第一次执行的。若这两条 CHECK 不含 bytedance，它就会在 267/268
	// 已经放行 bytedance 之后把约束重新收窄——user_platform_quotas 上因存量行直接 ADD
	// CONSTRAINT 失败（启动崩溃循环），composite_model_routes 上无声抹掉 268 已授予的取值。
	require.Contains(t, sql,
		"CHECK (platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'kimi', 'zhipu', 'deepseek', 'minimax', 'bytedance'))")
	require.Contains(t, sql,
		"CHECK (target_platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'kimi', 'zhipu', 'deepseek', 'minimax', 'windsurf', 'opencode', 'bytedance'))")
	require.Contains(t, sql,
		"CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok', 'antigravity', 'kimi', 'zhipu', 'deepseek', 'minimax', 'glm', 'windsurf', 'opencode'))")
}
