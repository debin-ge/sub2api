//go:build unit

package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMatchesModelFamilyRejectsHigherVersions 覆盖 matchesModelFamily 的两条边界。
func TestMatchesModelFamilyRejectsHigherVersions(t *testing.T) {
	cases := []struct {
		model  string
		family string
		want   bool
	}{
		{model: "glm-5", family: "glm-5", want: true},
		{model: "zai/glm-5", family: "glm-5", want: true},
		{model: "glm-5-turbo", family: "glm-5", want: true}, // 后缀是词不是版本号
		{model: "glm-5.2", family: "glm-5", want: false},    // 点号版本延续
		{model: "glm-5-1", family: "glm-5", want: false},    // 短横线版本延续
		{model: "glm-52", family: "glm-5", want: false},     // 数字直接延续
		{model: "myglm-5", family: "glm-5", want: false},    // 前面紧邻字母
		{model: "kimi-k2", family: "kimi-k2", want: true},
		{model: "kimi-k2-thinking", family: "kimi-k2", want: true},
		{model: "kimi-k2-0905", family: "kimi-k2", want: false},
		{model: "kimi-k2.7", family: "kimi-k2", want: false},
		{model: "minimax-m3", family: "minimax-m3", want: true},
		{model: "minimax-m3.1", family: "minimax-m3", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.model+"/"+tc.family, func(t *testing.T) {
			require.Equal(t, tc.want, matchesModelFamily(tc.model, tc.family))
		})
	}
}

// TestGetModelPricing_WindsurfDashSpellingGLM51 是一个真实存在的错价：
// Windsurf 官方模型表用 glm-5-1（见 windsurfOfficialModelIDs），而 GLM 的兜底分支
// 过去只认点号写法，于是它掉到 glm-5 档，按 $1.0/$3.2 而非 $1.4/$4.4 结算，少收约 29%。
func TestGetModelPricing_WindsurfDashSpellingGLM51(t *testing.T) {
	svc := newTestBillingService()

	got, err := svc.GetModelPricing("glm-5-1")
	require.NoError(t, err)
	require.NotNil(t, got)

	require.InDelta(t, 1.4e-6, got.InputPricePerToken, 1e-12)
	require.InDelta(t, 4.4e-6, got.OutputPricePerToken, 1e-12)
	require.InDelta(t, 0.26e-6, got.CacheReadPricePerToken, 1e-12)
}

// TestGetModelPricing_UnknownDomesticFamilyMemberIsNotDowngraded 固定住本次收紧的意图：
// 同族的未知新 SKU 宁可取不到价（调用方会看到 pricing not found），也不要被静默套上
// 低一档的老价——glm-5.2 当初就是这样按 glm-5 少收了约 29% 且毫无告警。
func TestGetModelPricing_UnknownDomesticFamilyMemberIsNotDowngraded(t *testing.T) {
	svc := newTestBillingService()

	for _, model := range []string{
		"glm-5.3",      // 未来的 GLM-5 系新版本
		"glm-4.8",      // 未来的 GLM-4 系新版本
		"kimi-k2.7",    // 未来的 K2 系新版本
		"kimi-k2-0905", // 官方未保留定价的历史快照
		"minimax-m2.9", // 未来的 M2 系新版本
		"minimax-m3.1", // 未来的 M3 系新版本
	} {
		t.Run(model, func(t *testing.T) {
			pricing, err := svc.GetModelPricing(model)
			require.Error(t, err, "unknown family member must not inherit a lower tier price")
			require.Nil(t, pricing)
		})
	}
}

// TestGetModelPricing_KnownDomesticSKUsKeepTheirPrice 是上一条的对照组：
// 收紧边界不能误伤已登记的型号。
func TestGetModelPricing_KnownDomesticSKUsKeepTheirPrice(t *testing.T) {
	svc := newTestBillingService()

	for _, model := range []string{
		"glm-5.2", "glm-5.1", "glm-5", "glm-5-turbo",
		"glm-4.7", "glm-4.6", "glm-4.5", "glm-4.5-air",
		"kimi-k2", "kimi-k2.6", "kimi-k2.5", "kimi-k2-thinking", "kimi-k3", "kimi-for-coding",
		"minimax-m3", "minimax-m2.7", "minimax-m2.7-highspeed", "minimax-m2.5", "minimax-m2.1", "minimax-m2",
		"deepseek-v4-pro", "deepseek-v4-flash",
	} {
		t.Run(model, func(t *testing.T) {
			pricing, err := svc.GetModelPricing(model)
			require.NoError(t, err)
			require.NotNil(t, pricing)
			require.Greater(t, pricing.InputPricePerToken, 0.0)
		})
	}
}

// TestGLMBillingNormalizersFollowCapabilityRegistry 确认计费侧的 GLM 型号清单已改为
// 从 domesticProviderCapabilities 派生，不再是散落各处的第二份硬编码列表。
func TestGLMBillingNormalizersFollowCapabilityRegistry(t *testing.T) {
	require.NotEmpty(t, glmBillingCanonicalIDs)

	for _, supported := range domesticProviderCapabilities[PlatformGLM].SupportedModelIDs {
		require.Contains(t, glmBillingCanonicalIDs, strings.ToLower(supported))
	}

	// 长的在前：glm-4.5-air 不能被将来加入的 glm-4.5 抢走。
	for i := 1; i < len(glmBillingCanonicalIDs); i++ {
		require.GreaterOrEqual(t, len(glmBillingCanonicalIDs[i-1]), len(glmBillingCanonicalIDs[i]))
	}

	require.Equal(t, "glm-5.2", normalizeGLMBillingModelStrict("GLM-5.2"))
	require.Equal(t, "glm-5.2", normalizeGLMBillingModelStrict("glm-5.2-250901"))
	require.Equal(t, "", normalizeGLMBillingModelStrict("glm-5.2-experimental"))
	require.Equal(t, "glm-4.5-air", normalizeGLMBillingModel("GLM-4.5-Air"))
	require.Equal(t, "", normalizeGLMBillingModel("glm-6"))
}
