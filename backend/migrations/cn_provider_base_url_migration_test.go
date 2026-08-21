package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCNProviderBaseURLBackfillMigration(t *testing.T) {
	content, err := FS.ReadFile("232_backfill_cn_provider_base_url.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "jsonb_set")
	require.Contains(t, sql, "{base_url}")
	require.Contains(t, sql, "base_url_openai")
	require.Contains(t, sql, "base_url_anthropic")
	require.Contains(t, sql, "api_protocol")
	require.Contains(t, sql, "platform IN ('kimi', 'zhipu', 'glm', 'deepseek')")
	require.Contains(t, sql, "COALESCE(credentials->>'base_url', '') = ''")
}
