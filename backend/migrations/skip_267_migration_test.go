package migrations

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	skip267MigrationFile     = "266a_skip_267_user_platform_quotas_bytedance.sql"
	bytedanceQuotaMigration  = "267_user_platform_quotas_add_bytedance.sql"
	quotaPlatformSupersetSQL = "273_user_platform_quotas_platform_superset.sql"
)

func migrationChecksum(t *testing.T, name string) string {
	t.Helper()
	body, err := FS.ReadFile(name)
	require.NoError(t, err)
	// 与 internal/repository/migrations_runner.go 的算法保持一致：TrimSpace 后取 SHA256。
	sum := sha256.Sum256([]byte(strings.TrimSpace(string(body))))
	return hex.EncodeToString(sum[:])
}

// 266a 把 267 登记为已应用来跳过它。登记的 checksum 一旦与 267 的实际内容对不上，
// runner 会在跳过之后立刻抛 checksum mismatch，症状与它本要修复的故障一模一样。
func TestSkip267MigrationPinsCurrentChecksum(t *testing.T) {
	want := migrationChecksum(t, bytedanceQuotaMigration)

	body, err := FS.ReadFile(skip267MigrationFile)
	require.NoError(t, err)
	sql := string(body)

	require.Contains(t, sql, want,
		"266a 登记的 checksum 必须等于 %s 的当前内容校验和（%s）", bytedanceQuotaMigration, want)
	require.Contains(t, sql, "'"+bytedanceQuotaMigration+"'")
	require.Contains(t, strings.ToLower(sql), "on conflict (filename) do nothing",
		"必须幂等：test 血脉的库里 267 已有记录")
}

// 跳过只能通过 schema_migrations 记账实现，不能顺手改表或改约束 ——
// 真正的终态由 273 负责。
func TestSkip267MigrationOnlyWritesMigrationLedger(t *testing.T) {
	body, err := FS.ReadFile(skip267MigrationFile)
	require.NoError(t, err)
	sql := stripSQLComments(string(body))
	lower := strings.ToLower(sql)

	require.Contains(t, lower, "insert into schema_migrations")
	for _, forbidden := range []string{"alter table", "create table", "drop ", "update ", "delete "} {
		require.NotContains(t, lower, forbidden,
			"266a 只应写 schema_migrations，不应出现 %q", forbidden)
	}
	require.NotContains(t, lower, "user_platform_quotas_platform_check")
}

// runner 按文件名排序执行（sort.Strings），266a 必须排在 267 之前才谈得上「跳过」。
func TestSkip267MigrationSortsBeforeTarget(t *testing.T) {
	names := []string{bytedanceQuotaMigration, skip267MigrationFile, "266_video_task_provider_url.sql"}
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)
	require.Equal(t,
		[]string{"266_video_task_provider_url.sql", skip267MigrationFile, bytedanceQuotaMigration},
		sorted)
}

// 跳过 267 的安全性前提：273 重建的平台集是 267 的超集，终态不会因此丢掉 bytedance。
func TestQuotaPlatformSupersetCovers267(t *testing.T) {
	narrowed := quotaCheckPlatforms(t, bytedanceQuotaMigration)
	superset := quotaCheckPlatforms(t, quotaPlatformSupersetSQL)

	require.NotEmpty(t, narrowed)
	for _, platform := range narrowed {
		require.Contains(t, superset, platform,
			"%s 缺少 267 已放行的平台 %q，跳过 267 会丢失该平台", quotaPlatformSupersetSQL, platform)
	}
	require.Contains(t, superset, "minimax", "273 必须放行 minimax —— 生产库里就有这样的配额行")
	require.Contains(t, superset, "bytedance")
}

var quotaCheckClauseRe = regexp.MustCompile(`(?is)ADD\s+CONSTRAINT\s+user_platform_quotas_platform_check\s+CHECK\s*\(\s*platform\s+IN\s*\((.*?)\)`)

func quotaCheckPlatforms(t *testing.T, name string) []string {
	t.Helper()
	body, err := FS.ReadFile(name)
	require.NoError(t, err)
	match := quotaCheckClauseRe.FindStringSubmatch(stripSQLComments(string(body)))
	require.Len(t, match, 2, "%s 未找到 user_platform_quotas_platform_check 的 ADD CONSTRAINT", name)

	var platforms []string
	for _, raw := range regexp.MustCompile(`'([^']*)'`).FindAllStringSubmatch(match[1], -1) {
		platforms = append(platforms, raw[1])
	}
	return platforms
}

var sqlLineCommentRe = regexp.MustCompile(`(?m)--[^\n]*`)

func stripSQLComments(sql string) string {
	return sqlLineCommentRe.ReplaceAllString(sql, "")
}
