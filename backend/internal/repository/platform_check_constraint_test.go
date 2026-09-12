package repository

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

// latestPlatformCheckConstraint 返回按文件名排序最后一个定义 constraintName 的迁移
// 名称，以及它在 CHECK (<column> IN (...)) 里列出的取值集合。迁移按文件名排序执行
// （见 migrations/README.md），所以「最后定义者」就是数据库最终生效的那份约束。
func latestPlatformCheckConstraint(t *testing.T, constraintName, column string) (string, map[string]struct{}) {
	t.Helper()

	entries, err := migrations.FS.ReadDir(".")
	require.NoError(t, err)

	checkRe := regexp.MustCompile(`ADD CONSTRAINT ` + regexp.QuoteMeta(constraintName) +
		`\s+CHECK \(` + regexp.QuoteMeta(column) + ` IN \(([^)]+)\)\)`)
	valueRe := regexp.MustCompile(`'([a-z0-9_.-]+)'`)

	var latestName string
	var latestValues map[string]struct{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		content, readErr := migrations.FS.ReadFile(entry.Name())
		require.NoError(t, readErr)
		match := checkRe.FindSubmatch(content)
		if match == nil || entry.Name() <= latestName {
			continue
		}
		values := make(map[string]struct{})
		for _, value := range valueRe.FindAllSubmatch(match[1], -1) {
			values[string(value[1])] = struct{}{}
		}
		latestName = entry.Name()
		latestValues = values
	}

	require.NotEmpty(t, latestName, "no migration defines %s", constraintName)
	require.NotEmpty(t, latestValues, "%s in %s lists no values", constraintName, latestName)
	return latestName, latestValues
}

// Scenario: 平台 CHECK 约束按文件名排序执行，后落地的迁移若基于过时的平台集合重建
// 约束，会静默抹掉先前迁移已加入的平台。267（加 bytedance）基于 224 的集合重建，
// 抹掉了 237 加入的 minimax；两者分处不同文件，git 合并时没有任何冲突可供发现。
func TestUserPlatformQuotaCheckMatchesAllowedQuotaPlatforms(t *testing.T) {
	name, values := latestPlatformCheckConstraint(t, "user_platform_quotas_platform_check", "platform")

	allowed := make(map[string]struct{}, len(service.AllowedQuotaPlatforms))
	for _, platform := range service.AllowedQuotaPlatforms {
		allowed[platform] = struct{}{}
		require.Contains(t, values, platform,
			"最后定义该约束的迁移 %s 丢掉了 service.AllowedQuotaPlatforms 允许的平台 %q；"+
				"请新开一个迁移把平台集合补成全集，不要修改已落库的迁移", name, platform)
	}

	extra := make([]string, 0, len(values))
	for platform := range values {
		if _, ok := allowed[platform]; !ok {
			extra = append(extra, platform)
		}
	}
	sort.Strings(extra)
	require.Empty(t, extra,
		"最后定义该约束的迁移 %s 放行了 service.AllowedQuotaPlatforms 不认的平台 %v", name, extra)
}

// Scenario: 复合路由的目标平台必须涵盖所有可配额平台，否则分组能配出该平台的配额，
// 却建不了指向它的复合路由。该集合允许比配额集合更宽（含 windsurf / opencode）。
func TestCompositeRouteTargetPlatformCheckCoversQuotaPlatforms(t *testing.T) {
	name, values := latestPlatformCheckConstraint(t, "composite_model_routes_target_platform_check", "target_platform")

	for _, platform := range service.AllowedQuotaPlatforms {
		require.Contains(t, values, platform,
			"最后定义该约束的迁移 %s 未涵盖可配额平台 %q", name, platform)
	}
}
