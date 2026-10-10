package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// 277 只能放宽两条平台 CHECK（超集），不得改数据、建表或收窄集合。
func TestFlovaPlatformMigrationOnlyExtendsChecks(t *testing.T) {
	body, err := FS.ReadFile("277_add_flova_platform.sql")
	require.NoError(t, err)
	sql := strings.ToLower(string(body))
	require.Contains(t, sql, "'flova'")
	require.Equal(t, 2, strings.Count(sql, "add constraint"))
	require.Equal(t, 2, strings.Count(sql, "drop constraint if exists"))
	require.NotContains(t, sql, " update ")
	require.NotContains(t, sql, " insert ")
	require.NotContains(t, sql, "create table")
	// 只允许在注释里提到 channel_monitors，不得 ALTER 它。
	require.NotContains(t, sql, "alter table channel_monitors")

	// 276 的全集必须原样保留（超集契约）。
	prev, err := FS.ReadFile("276_platform_superset_media_vendors.sql")
	require.NoError(t, err)
	for _, platform := range []string{"anthropic", "openai", "gemini", "antigravity", "grok", "kimi", "zhipu",
		"deepseek", "minimax", "bytedance", "opencode_go", "fal", "wavespeed", "runware", "windsurf"} {
		require.Contains(t, strings.ToLower(string(prev)), "'"+platform+"'")
		require.Contains(t, sql, "'"+platform+"'")
	}
}
