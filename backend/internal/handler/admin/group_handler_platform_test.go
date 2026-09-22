//go:build unit

package admin

import (
	"bytes"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 回归分组平台枚举:kimi/zhipu/deepseek/opencode_go 必须能通过 Create/Update 的
// binding 校验（历史 bug:调度/路由链路已支持这些平台,但 oneof 白名单漏加,
// 导致平台分组无法创建、账号"无可用分组"）;非法值仍须被拒。
func bindGroupPlatformJSON(t *testing.T, target any, body string) error {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c.ShouldBindJSON(target)
}

func TestGroupPlatformBinding_AllowedPlatforms(t *testing.T) {
	allowed := []string{
		"anthropic", "openai", "gemini", "antigravity", "grok",
		"kimi", "zhipu", "deepseek", "minimax", "windsurf", "opencode_go", "composite",
	}
	for _, platform := range allowed {
		t.Run("create_"+platform, func(t *testing.T) {
			var req CreateGroupRequest
			body := fmt.Sprintf(`{"name":"g","platform":%q}`, platform)
			require.NoError(t, bindGroupPlatformJSON(t, &req, body),
				"platform %q 应通过 CreateGroupRequest 校验", platform)
			require.Equal(t, platform, req.Platform)
		})
		t.Run("update_"+platform, func(t *testing.T) {
			var req UpdateGroupRequest
			body := fmt.Sprintf(`{"platform":%q}`, platform)
			require.NoError(t, bindGroupPlatformJSON(t, &req, body),
				"platform %q 应通过 UpdateGroupRequest 校验", platform)
			require.Equal(t, platform, req.Platform)
		})
	}
}

// glm 是 zhipu 的历史平台 ID：新建分组禁止使用，但编辑历史分组必须继续可用。
func TestGroupPlatformBinding_GLMCreateBlockedUpdateAllowed(t *testing.T) {
	var createReq CreateGroupRequest
	require.Error(t, bindGroupPlatformJSON(t, &createReq, `{"name":"g","platform":"glm"}`),
		"platform \"glm\" 应被 CreateGroupRequest 拒绝（新建分组禁止使用历史平台 ID）")

	var updateReq UpdateGroupRequest
	require.NoError(t, bindGroupPlatformJSON(t, &updateReq, `{"platform":"glm"}`),
		"platform \"glm\" 应仍能通过 UpdateGroupRequest 校验（保留历史分组编辑能力）")
	require.Equal(t, "glm", updateReq.Platform)
}

func TestGroupPlatformBinding_RejectsInvalidPlatforms(t *testing.T) {
	invalid := []string{
		"moonshot", // 厂商别名,不是平台标识
		"Kimi",     // 大小写敏感
		"openai ",  // 尾随空格
		"bogus",
	}
	for _, platform := range invalid {
		t.Run("create_"+platform, func(t *testing.T) {
			var req CreateGroupRequest
			body := fmt.Sprintf(`{"name":"g","platform":%q}`, platform)
			require.Error(t, bindGroupPlatformJSON(t, &req, body),
				"platform %q 应被 CreateGroupRequest 拒绝", platform)
		})
		t.Run("update_"+platform, func(t *testing.T) {
			var req UpdateGroupRequest
			body := fmt.Sprintf(`{"platform":%q}`, platform)
			require.Error(t, bindGroupPlatformJSON(t, &req, body),
				"platform %q 应被 UpdateGroupRequest 拒绝", platform)
		})
	}
}

func TestCompositeRouteTargetPlatform_AllowsCNProviders(t *testing.T) {
	for _, platform := range []string{"kimi", "zhipu", "deepseek", "minimax", "opencode_go"} {
		var req CompositeRouteRequest
		body := fmt.Sprintf(`{"public_model":"m","target_platform":%q}`, platform)
		require.NoError(t, bindGroupPlatformJSON(t, &req, body))
		require.Equal(t, platform, req.TargetPlatform)
	}
}

func TestCompositeRouteTargetPlatform_RejectsComposite(t *testing.T) {
	var req CompositeRouteRequest
	require.Error(t, bindGroupPlatformJSON(t, &req, `{"public_model":"m","target_platform":"composite"}`))
}

// composite 路由的目标平台白名单不含 glm：DB 的 CHECK 约束已在创建/更新两侧
// 都排除该历史平台 ID，绑定层需与之保持一致。
func TestCompositeRouteTargetPlatform_RejectsGLM(t *testing.T) {
	var req CompositeRouteRequest
	require.Error(t, bindGroupPlatformJSON(t, &req, `{"public_model":"m","target_platform":"glm"}`))
}
