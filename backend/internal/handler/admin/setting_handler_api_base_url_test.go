package admin

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/stretchr/testify/require"
)

// api_base_url 会被文档页/广场直接替换进链接与代码示例：非 http(s)、带凭证或缺 host 的值必须在保存时拒绝。
func TestUpdateSettingsRejectsNonHTTPAPIBaseURL(t *testing.T) {
	for _, raw := range []string{
		"javascript:alert(1)//",
		"data:text/html,hi",
		"ftp://example.com/",
		"https://user:pass@example.com/",
		"https:///no-host",
		"not a url",
	} {
		t.Run(raw, func(t *testing.T) {
			h, repo := newStepUpSwitchTestHandler(t, map[string]string{})

			rec := doUpdateSettings(t, h, map[string]any{"api_base_url": raw}, nil)

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Contains(t, rec.Body.String(), "api_base_url")
			require.Empty(t, repo.values[service.SettingKeyAPIBaseURL])
		})
	}
}

func TestUpdateSettingsAcceptsHTTPAPIBaseURL(t *testing.T) {
	for _, raw := range []string{"https://api.example.com/", "http://10.0.0.5:8080", ""} {
		t.Run(raw, func(t *testing.T) {
			h, _ := newStepUpSwitchTestHandler(t, map[string]string{})

			rec := doUpdateSettings(t, h, map[string]any{"api_base_url": raw}, nil)

			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		})
	}
}
