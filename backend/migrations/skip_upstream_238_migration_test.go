package migrations

import (
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	skipUpstream238File    = "238_a_skip_upstream_platform_migrations.sql"
	upstreamOpenCodeGoSQL  = "238_opencode_go_platform.sql"
	upstreamPurgeQuotasSQL = "238_purge_unlimited_user_platform_quotas.sql"
	platformSupersetGoSQL  = "274_platform_superset_opencode_go.sql"
)

// 238_a 把 upstream v0.2.7 的两条 238 登记为已应用来跳过它们。登记的 checksum 一旦与
// 实际内容对不上，runner 会在跳过之后立刻抛 checksum mismatch，症状与它本要修复的
// 崩溃循环一模一样。
func TestSkipUpstream238MigrationPinsCurrentChecksums(t *testing.T) {
	body, err := FS.ReadFile(skipUpstream238File)
	require.NoError(t, err)
	sql := string(body)

	for _, target := range []string{upstreamOpenCodeGoSQL, upstreamPurgeQuotasSQL} {
		want := migrationChecksum(t, target)
		require.Contains(t, sql, want,
			"238_a 登记的 checksum 必须等于 %s 的当前内容校验和（%s）", target, want)
		require.Contains(t, sql, "'"+target+"'")
	}

	require.Contains(t, strings.ToLower(sql), "on conflict (filename) do nothing",
		"必须幂等：已应用过的库要退化为空操作")
}

// 跳过只能通过 schema_migrations 记账实现，不能顺手改表或改约束 ——
// 真正的终态由 274 负责。
func TestSkipUpstream238MigrationOnlyWritesMigrationLedger(t *testing.T) {
	body, err := FS.ReadFile(skipUpstream238File)
	require.NoError(t, err)
	lower := strings.ToLower(stripSQLComments(string(body)))

	require.Contains(t, lower, "insert into schema_migrations")
	for _, forbidden := range []string{"alter table", "create table", "drop ", "update ", "delete "} {
		require.NotContains(t, lower, forbidden,
			"238_a 只应写 schema_migrations 账本，终态由 %s 负责", platformSupersetGoSQL)
	}
}

// runner 按文件名 sort.Strings 排序执行，238_a 必须严格排在两条 upstream 238 之前，
// 否则它们会先跑一步，跳过就失效了。
func TestSkipUpstream238SortsBeforeUpstreamMigrations(t *testing.T) {
	names := []string{upstreamPurgeQuotasSQL, skipUpstream238File, upstreamOpenCodeGoSQL}
	sort.Strings(names)
	require.Equal(t, skipUpstream238File, names[0],
		"238_a 必须排在两条 upstream 238 之前")
}

// 274 是平台集终态权威：必须排在 273 之后，且同时含 fork 的 bytedance 与 upstream 的
// opencode_go，并且已经把存量 'opencode' 归一掉。
func TestPlatformSupersetMigrationCarriesBothLineages(t *testing.T) {
	names := []string{platformSupersetGoSQL, quotaPlatformSupersetSQL}
	sort.Strings(names)
	require.Equal(t, quotaPlatformSupersetSQL, names[0],
		"274 必须排在 273 之后才能写定终态")

	body, err := FS.ReadFile(platformSupersetGoSQL)
	require.NoError(t, err)
	sql := stripSQLComments(string(body))

	for _, platform := range []string{"'bytedance'", "'opencode_go'", "'minimax'", "'windsurf'", "'glm'"} {
		require.Contains(t, sql, platform, "终态平台集缺少 %s", platform)
	}
	require.NotContains(t, sql, "'opencode',",
		"存量 'opencode' 应已归一为 'opencode_go'，不应再出现在任何平台集中")
	require.Contains(t, strings.ToLower(sql), "set platform = 'opencode_go'",
		"必须在收紧约束之前改写存量 accounts.platform")
}
