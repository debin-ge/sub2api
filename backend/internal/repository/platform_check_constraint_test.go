package repository

import (
	"github.com/Wei-Shaw/sub2api/ent/userplatformquota"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
	"regexp"
	"strings"
	"testing"
)

// Both migration lineages must finish with the registry owning membership; a later
// historical ADD must never silently discard local video or new upstream platforms.
func TestPlatformConstraintsFinishWithRegistryOwnership(t *testing.T) {
	entries, err := migrations.FS.ReadDir(".")
	require.NoError(t, err)
	for _, name := range []string{"user_platform_quotas_platform_check", "composite_model_routes_target_platform_check"} {
		action := ""
		source := ""
		pattern := regexp.MustCompile(`(?i)(ADD|DROP) CONSTRAINT (?:IF EXISTS )?` + name)
		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), ".sql") {
				continue
			}
			sql, err := migrations.FS.ReadFile(entry.Name())
			require.NoError(t, err)
			for _, match := range pattern.FindAllStringSubmatch(string(sql), -1) {
				action = strings.ToUpper(match[1])
				source = entry.Name()
			}
		}
		require.Equal(t, "DROP", action, "%s final action in %s must leave application registry authoritative", name, source)
	}
	require.ElementsMatch(t, domain.ConcretePlatformIDs(), service.AllowedQuotaPlatforms)
	for _, platform := range service.AllowedQuotaPlatforms {
		require.NoError(t, userplatformquota.PlatformValidator(platform), platform)
	}
	for _, invalid := range []string{"", "unknown", "composite", "glm"} {
		require.Error(t, userplatformquota.PlatformValidator(invalid), invalid)
	}
}
