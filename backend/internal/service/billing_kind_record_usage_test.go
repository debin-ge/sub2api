package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 这一组用例守的是同一件事：**结算口径由入口决定，不由上游返回值决定**。
//
// 历史实现从 result 反推口径（WebSearchCalls>0 / VideoCount>0 且模型名像 grok
// video / ImageCount>0）。于是"上游少回一个字段"等价于"换一张价格表"：videos_*
// 路由拿不到 video_count 就掉进 token 分支，而 grok-imagine-video 根本没有 token
// 价，一条已经生成并交付的视频最终被记成 $0。
//
// 反方向必须保留：/v1/chat/completions 上的出图模型是真的会在一次对话里产出图片，
// token 只是基线口径，仍然接受上游产出的媒体升级结算方式。

func newGrokVideoBillingAPIKeyForTest(groupID int64, videoPrice480P *float64) *APIKey {
	return &APIKey{
		ID:      10500,
		GroupID: i64p(groupID),
		Group: &Group{
			ID:                   groupID,
			Platform:             PlatformGrok,
			RateMultiplier:       1,
			VideoRateIndependent: true,
			VideoRateMultiplier:  1,
			VideoPrice480P:       videoPrice480P,
		},
	}
}

// 上游漏回 video_count 是这次修复的原始现场：不带显式口径时它会被结算成 token，
// 而该模型没有 token 价，于是整条视频免费。
func TestRecordUsage_VideoKindSurvivesMissingUpstreamVideoCount(t *testing.T) {
	videoPrice480P := 0.08
	groupID := int64(500)

	t.Run("explicit_video_kind_bills_by_video", func(t *testing.T) {
		usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
		svc := newOpenAIRecordUsageServiceForTest(usageRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)

		err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
			Result: &OpenAIForwardResult{
				RequestID:            "video-missing-count",
				Model:                "grok-imagine-video-1.5",
				BillingModel:         "grok-imagine-video-1.5",
				VideoCount:           0, // 上游没回
				VideoResolution:      VideoBillingResolution480P,
				VideoDurationSeconds: 1,
				Duration:             time.Second,
			},
			APIKey:      newGrokVideoBillingAPIKeyForTest(groupID, &videoPrice480P),
			User:        &User{ID: 20500},
			Account:     &Account{ID: 30500, Platform: PlatformGrok},
			BillingKind: BillingKindVideo,
		})

		require.NoError(t, err)
		require.NotNil(t, usageRepo.lastLog)
		require.Equal(t, BillingStateSettled, usageRepo.lastLog.BillingState)
		require.InDelta(t, videoPrice480P, usageRepo.lastLog.TotalCost, 1e-12)
		require.NotNil(t, usageRepo.lastLog.BillingMode)
		require.Equal(t, string(BillingModeVideo), *usageRepo.lastLog.BillingMode)
		// 计费按 1 个视频算，用量行也必须写 1，否则"扣了 1 个的钱、记了 0 个的量"对不上账。
		require.Equal(t, 1, usageRepo.lastLog.VideoCount)
	})

	t.Run("without_explicit_kind_it_falls_through_to_token_and_finds_no_price", func(t *testing.T) {
		usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
		svc := newOpenAIRecordUsageServiceForTest(usageRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)

		err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
			Result: &OpenAIForwardResult{
				RequestID:            "video-missing-count-legacy",
				Model:                "grok-imagine-video-1.5",
				BillingModel:         "grok-imagine-video-1.5",
				VideoCount:           0,
				VideoResolution:      VideoBillingResolution480P,
				VideoDurationSeconds: 1,
				Duration:             time.Second,
			},
			APIKey:  newGrokVideoBillingAPIKeyForTest(groupID, &videoPrice480P),
			User:    &User{ID: 20501},
			Account: &Account{ID: 30501, Platform: PlatformGrok},
		})

		// 这是旧行为的现场快照：记录留下了（P0-3 的 fail-loud），但一分钱没收到。
		// 它存在的意义是证明上面那条用例修的确实是这个洞。
		require.NoError(t, err)
		require.NotNil(t, usageRepo.lastLog)
		require.Equal(t, BillingStatePricingUnavailable, usageRepo.lastLog.BillingState)
		require.Zero(t, usageRepo.lastLog.TotalCost)
		require.Zero(t, usageRepo.lastLog.VideoCount)
	})
}

// 显式视频口径不该压过管理员的意图：渠道明确配了 token 价就按 token 结算。
func TestRecordUsage_VideoKindYieldsToExplicitChannelTokenPricing(t *testing.T) {
	groupID := int64(502)
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	svc := newOpenAIRecordUsageServiceForTest(usageRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	svc.resolver = newOpenAITokenImageChannelPricingResolverForTest(t, groupID, "grok-imagine-video")

	err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{
			RequestID:            "video-channel-token",
			Model:                "grok-imagine-video",
			BillingModel:         "grok-imagine-video",
			Usage:                OpenAIUsage{InputTokens: 1000, OutputTokens: 1000},
			VideoCount:           1,
			VideoResolution:      VideoBillingResolution480P,
			VideoDurationSeconds: 1,
			Duration:             time.Second,
		},
		APIKey:      &APIKey{ID: 10502, GroupID: i64p(groupID), Group: &Group{ID: groupID, Platform: PlatformGrok, RateMultiplier: 1}},
		User:        &User{ID: 20502},
		Account:     &Account{ID: 30502, Platform: PlatformGrok},
		BillingKind: BillingKindVideo,
	})

	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.NotNil(t, usageRepo.lastLog.BillingMode)
	require.Equal(t, string(BillingModeToken), *usageRepo.lastLog.BillingMode)
	require.Greater(t, usageRepo.lastLog.TotalCost, 0.0)
}

// token 是基线而非终点：对话路由上真产出的图片仍按张结算。
// 若把 BillingKindToken 做成"只能按 token 结算"，出图模型会立刻少收钱。
func TestRecordUsage_TokenKindStillUpgradesToImageBilling(t *testing.T) {
	imagePrice2K := 0.4
	groupID := int64(503)

	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	svc := newOpenAIRecordUsageServiceForTest(usageRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)

	err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{
			RequestID:    "chat-produced-image",
			Model:        "gemini-image",
			BillingModel: "gemini-image",
			ImageCount:   2,
			ImageSize:    "2K",
			Duration:     time.Second,
		},
		APIKey: &APIKey{
			ID:      10503,
			GroupID: i64p(groupID),
			Group: &Group{
				ID:                   groupID,
				RateMultiplier:       1,
				ImageRateIndependent: true,
				ImageRateMultiplier:  1,
				ImagePrice2K:         &imagePrice2K,
			},
		},
		User:        &User{ID: 20503},
		Account:     &Account{ID: 30503},
		BillingKind: BillingKindToken,
	})

	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.NotNil(t, usageRepo.lastLog.BillingMode)
	require.Equal(t, string(BillingModeImage), *usageRepo.lastLog.BillingMode)
	require.InDelta(t, 2*imagePrice2K, usageRepo.lastLog.TotalCost, 1e-12)
}

// BillingKindNone 是明确的端点级非计费白名单，与"查不到价格"是两件事：
// 白名单端点不该因为模型没配价而被标成待结算。
func TestRecordUsage_NoneKindIsFreeWithoutPricingLookup(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	svc := newOpenAIRecordUsageServiceForTest(usageRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)

	err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{
			RequestID:    "count-tokens",
			Model:        "totally-unknown-model",
			BillingModel: "totally-unknown-model",
			Usage:        OpenAIUsage{InputTokens: 1234},
			Duration:     time.Millisecond,
		},
		APIKey:      &APIKey{ID: 10504, Group: &Group{RateMultiplier: 1}},
		User:        &User{ID: 20504},
		Account:     &Account{ID: 30504},
		BillingKind: BillingKindNone,
	})

	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.Equal(t, BillingStateSettled, usageRepo.lastLog.BillingState)
	require.Zero(t, usageRepo.lastLog.TotalCost)
	require.Equal(t, 1234, usageRepo.lastLog.InputTokens)
}

// 网页搜索按次计费，上游不返回 usage/token。没有显式口径时它靠 WebSearchCalls>0
// 反推——上游漏回计数就会掉进 token 分支。
func TestRecordUsage_WebSearchKindDoesNotFallThroughToTokenPricing(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	svc := newOpenAIRecordUsageServiceForTest(usageRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)

	err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{
			RequestID:      "alpha-search",
			Model:          "totally-unknown-search-model",
			BillingModel:   "totally-unknown-search-model",
			WebSearchCalls: 3,
			Duration:       time.Second,
		},
		APIKey:      &APIKey{ID: 10505, Group: &Group{RateMultiplier: 1}},
		User:        &User{ID: 20505},
		Account:     &Account{ID: 30505},
		BillingKind: BillingKindWebSearch,
	})

	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.Equal(t, BillingStateSettled, usageRepo.lastLog.BillingState)
	// 未配置分组单价时按官方默认 $10/1000 次。
	require.InDelta(t, 3*defaultWebSearchPricePerCall, usageRepo.lastLog.TotalCost, 1e-12)
}
