//go:build unit

package service

import (
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestSelectBillingModelBySource(t *testing.T) {
	tests := []struct {
		name           string
		source         string
		want           string
		wantRecognized bool
	}{
		{
			name:           "requested",
			source:         BillingModelSourceRequested,
			want:           "public-alias",
			wantRecognized: true,
		},
		{
			name:           "channel mapped",
			source:         BillingModelSourceChannelMapped,
			want:           "channel-alias",
			wantRecognized: true,
		},
		{
			name:           "upstream",
			source:         BillingModelSourceUpstream,
			want:           "kimi-k3",
			wantRecognized: true,
		},
		{
			// 空来源只发生在整条链上根本没有渠道的场景（Channel 进内存时会被
			// normalizeBillingModelSource 回填成 channel_mapped）。此时调用方必须
			// 保留自己的基准模型，不能被覆盖成空串。
			name:   "empty source is not recognized",
			source: "",
		},
		{
			name:   "unknown source is not recognized",
			source: "some-future-source",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, recognized := selectBillingModelBySource(
				tt.source,
				"  public-alias  ",
				"channel-alias",
				"kimi-k3",
			)
			require.Equal(t, tt.wantRecognized, recognized)
			require.Equal(t, tt.want, got)
		})
	}
}

// legacySettlementBillingModel 是 P1-2 重构前 recordUsageCore 里那段 if/else 链的
// 逐字复制，用来把新旧口径逐组合对齐：selectBillingModelBySource 只该把映射表挪个
// 地方，不该改变任何一笔账的计费模型。
func legacySettlementBillingModel(source, concrete, originalModel, channelMappedModel, upstreamModel string, isMiniMax bool) string {
	billingModel := concrete
	if source == BillingModelSourceUpstream {
		if upstream := strings.TrimSpace(upstreamModel); upstream != "" {
			billingModel = upstream
		}
	} else if !isMiniMax {
		if source == BillingModelSourceChannelMapped && channelMappedModel != "" {
			billingModel = channelMappedModel
		}
		if source == BillingModelSourceRequested && originalModel != "" {
			billingModel = originalModel
		}
	}
	return billingModel
}

// currentSettlementBillingModel 复刻 recordUsageCore 里现在这段（共用映射表 + 结算侧
// 的空值/MiniMax 策略）。两个函数必须对所有组合给出同一个答案。
func currentSettlementBillingModel(source, concrete, originalModel, channelMappedModel, upstreamModel string, isMiniMax bool) string {
	billingModel := concrete
	if selected, ok := selectBillingModelBySource(source, originalModel, channelMappedModel, upstreamModel); ok && selected != "" {
		if source == BillingModelSourceUpstream || !isMiniMax {
			billingModel = selected
		}
	}
	return billingModel
}

func TestSelectBillingModelBySource_MatchesLegacySettlementDerivation(t *testing.T) {
	sources := []string{
		BillingModelSourceRequested,
		BillingModelSourceChannelMapped,
		BillingModelSourceUpstream,
		"",
		"some-future-source",
	}
	inputs := []struct {
		name               string
		originalModel      string
		channelMappedModel string
		upstreamModel      string
	}{
		{
			name:               "all three distinct",
			originalModel:      "public-alias",
			channelMappedModel: "channel-alias",
			upstreamModel:      "kimi-k3",
		},
		{
			name:          "no channel mapping",
			originalModel: "public-alias",
			upstreamModel: "kimi-k3",
		},
		{
			// 上游模型未知时（例如 handler 没能从响应里读出来）结算必须回落到
			// 基准模型，而不是拿空串去查价。
			name:               "upstream unknown",
			originalModel:      "public-alias",
			channelMappedModel: "channel-alias",
		},
		{
			name: "everything empty",
		},
	}

	for _, source := range sources {
		for _, in := range inputs {
			for _, isMiniMax := range []bool{false, true} {
				name := source + "/" + in.name
				if isMiniMax {
					name += "/minimax"
				}
				t.Run(name, func(t *testing.T) {
					// concrete 就是 recordUsageBillingModel 的结果，两侧共用同一个值。
					concrete := "concrete-model"
					require.Equal(t,
						legacySettlementBillingModel(source, concrete, in.originalModel, in.channelMappedModel, in.upstreamModel, isMiniMax),
						currentSettlementBillingModel(source, concrete, in.originalModel, in.channelMappedModel, in.upstreamModel, isMiniMax),
					)
				})
			}
		}
	}
}

// TestSelectBillingModelBySource_TrimsWhitespaceOnlyNames 记录新旧口径唯一的差别：
// 全空白的模型名此前会被原样赋给 billingModel（后续查价必然失败），现在按空值处理
// 回落到基准模型。差别只影响本来就查不出价的输入，方向是把记录落得更准。
func TestSelectBillingModelBySource_TrimsWhitespaceOnlyNames(t *testing.T) {
	const concrete = "concrete-model"
	require.Equal(t, "   ", legacySettlementBillingModel(
		BillingModelSourceChannelMapped, concrete, "public-alias", "   ", "kimi-k3", false))
	require.Equal(t, concrete, currentSettlementBillingModel(
		BillingModelSourceChannelMapped, concrete, "public-alias", "   ", "kimi-k3", false))
}

func TestBillingModelDrifted(t *testing.T) {
	tests := []struct {
		name     string
		admitted string
		settled  string
		upstream string
		want     bool
	}{
		{
			name:     "settlement stayed on the admitted model",
			admitted: "channel-alias",
			settled:  "channel-alias",
			upstream: "kimi-k3",
		},
		{
			// 模型名大小写不敏感（查价链入口就 ToLower），大小写差异不是漂移。
			name:     "case differences are not drift",
			admitted: "Channel-Alias",
			settled:  "channel-alias",
			upstream: "kimi-k3",
		},
		{
			// 上游模型是准入守卫本来就验过的第二个候选，落到它上面不算漂移。
			name:     "falling back to the upstream model is still admitted",
			admitted: "channel-alias",
			settled:  "kimi-k3",
			upstream: "kimi-k3",
		},
		{
			// composite 的具体模型、以及请求模型本身，都只在结算的候选集合里。
			name:     "falling back to the requested model leaves the admitted set",
			admitted: "all",
			settled:  "public-alias",
			upstream: "kimi-k3",
			want:     true,
		},
		{
			name:     "empty settled model is not a drift signal",
			admitted: "channel-alias",
			settled:  "",
			upstream: "kimi-k3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, billingModelDrifted(tt.admitted, tt.settled, tt.upstream))
		})
	}
}

func TestGatewayServiceReportBillingModelDrift(t *testing.T) {
	countSeen := func(svc *GatewayService) int {
		count := 0
		svc.billingModelDriftSeen.Range(func(any, any) bool {
			count++
			return true
		})
		return count
	}
	result := &ForwardResult{Model: "public-alias", UpstreamModel: "kimi-k3"}

	t.Run("records each admitted/settled pair once", func(t *testing.T) {
		svc := newGatewayPricingGuardService(nil)
		for i := 0; i < 3; i++ {
			svc.reportBillingModelDrift(nil, nil, BillingModelSourceChannelMapped, "all", "public-alias", result)
		}
		require.Equal(t, 1, countSeen(svc))

		// 另一组合是另一份配置问题，要单独可见。
		svc.reportBillingModelDrift(nil, nil, BillingModelSourceChannelMapped, "all", "another-model", result)
		require.Equal(t, 2, countSeen(svc))
	})

	t.Run("no drift records nothing", func(t *testing.T) {
		svc := newGatewayPricingGuardService(nil)
		svc.reportBillingModelDrift(nil, nil, BillingModelSourceChannelMapped, "kimi-k3", "kimi-k3", result)
		require.Equal(t, 0, countSeen(svc))
	})

	t.Run("simple mode bills nothing so drift is meaningless", func(t *testing.T) {
		svc := newGatewayPricingGuardService(&config.Config{RunMode: config.RunModeSimple})
		svc.reportBillingModelDrift(nil, nil, BillingModelSourceChannelMapped, "all", "public-alias", result)
		require.Equal(t, 0, countSeen(svc))
	})

	t.Run("nil forward result is a no-op", func(t *testing.T) {
		svc := newGatewayPricingGuardService(nil)
		svc.reportBillingModelDrift(nil, nil, BillingModelSourceChannelMapped, "all", "public-alias", nil)
		require.Equal(t, 0, countSeen(svc))
	})
}
