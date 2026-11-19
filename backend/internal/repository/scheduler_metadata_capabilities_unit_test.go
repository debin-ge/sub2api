//go:build unit

package repository

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// buildSchedulerMetadataAccount 是调度候选的唯一投影出口，openAIAccountSkipReason
// 按 SupportsOpenAIEndpointCapability 判 capability_mismatch。显式声明能力的分支直接读
// credentials.openai_capabilities 且不走「未配置即放行」，所以该键一旦被裁掉，
// 对应账号在选号阶段必然被剔除，而 /v1/models 走 DB 全量仍会列出模型。
//
// upstream 用 Seedance 能力举例；本 fork 已退役 OpenAI 形态的 Seedance（改走 Ark 任务协议），
// 这里改用同样「只认显式声明」的 videos 能力，断言的投影行为不变。
func TestSchedulerMetadataKeepsOpenAICapabilities(t *testing.T) {
	account := service.Account{
		ID:       1,
		Platform: service.PlatformOpenAI,
		Type:     service.AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":             "sk-test",
			"base_url":            "https://relay.example.com/api/v3",
			"openai_capabilities": []string{"videos"},
		},
	}

	projected := buildSchedulerMetadataAccount(account)

	require.Equal(t, []string{"videos"}, projected.Credentials["openai_capabilities"])
	require.Equal(t, "https://relay.example.com/api/v3", projected.Credentials["base_url"])
	require.True(t, projected.SupportsOpenAIEndpointCapability(service.OpenAIEndpointCapabilityVideos))
}

func TestSchedulerMetadataDropsUnlistedCredentials(t *testing.T) {
	account := service.Account{
		ID:       2,
		Platform: service.PlatformOpenAI,
		Type:     service.AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":             "sk-test",
			"base_url":            "https://relay.example.com/api/v3",
			"openai_capabilities": []string{"videos"},
			"model_mapping":       map[string]string{"seedance-2-5": "doubao-seedance-2-5-260628"},
			"unlisted_secret":     "drop-me",
		},
	}

	projected := buildSchedulerMetadataAccount(account)

	require.Equal(t, map[string]string{"seedance-2-5": "doubao-seedance-2-5-260628"}, projected.Credentials["model_mapping"])
	require.NotContains(t, projected.Credentials, "unlisted_secret")
}
