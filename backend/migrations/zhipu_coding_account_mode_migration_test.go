package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestZhipuCodingAccountModeBackfillMigration 校验 230 把无自定义 URL 的存量
// zhipu 账号回填为 Coding Plan，避免 229 改名后掉到按量付费端点。
func TestZhipuCodingAccountModeBackfillMigration(t *testing.T) {
	content, err := FS.ReadFile("230_backfill_zhipu_coding_account_mode.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "jsonb_set(credentials, '{account_mode}', '\"coding\"')")
	require.Contains(t, sql, "platform = 'zhipu'")
	require.Contains(t, sql, "credentials->>'account_mode'")
	require.Contains(t, sql, "credentials->>'base_url'")
	require.Contains(t, sql, "credentials->>'base_url_openai'")
}
