//go:build unit

package handler

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// Capture the real RecordUsage output at the durable repository boundary. The
// fake represents an already committed intent; post-effect projections need no
// external cache or database in this handler regression test.
type grokVideoBillingIntentRepo struct {
	service.DurableUsageBillingRepository
	commands []service.UsageBillingCommand
	logs     []service.UsageLog
	acks     int
}

func (r *grokVideoBillingIntentRepo) ApplyAndRecord(_ context.Context, cmd *service.UsageBillingCommand, log *service.UsageLog) (*service.UsageBillingApplyResult, error) {
	r.commands = append(r.commands, *cmd)
	r.logs = append(r.logs, *log)
	return &service.UsageBillingApplyResult{
		UsageLogRecorded: true,
		OutboxReceipt:    &service.UsageBillingOutboxReceipt{ID: int64(len(r.commands)), WorkerID: "grok-billing-test"},
	}, nil
}

func (r *grokVideoBillingIntentRepo) AcknowledgeUsageBillingOutbox(context.Context, string, int64) error {
	r.acks++
	return nil
}

func TestGrokVideoCompletionSettlesGenerationThroughFreeLookup(t *testing.T) {
	for _, firstLookup := range []string{"status", "content"} {
		for _, unitPrice := range []float64{0.125, 0} {
			name := firstLookup + "/paid"
			if unitPrice == 0 {
				name = firstLookup + "/explicitly-free"
			}
			t.Run(name, func(t *testing.T) {
				h, slots, bindings, upstream := newGrokMediaSlotHandler(t, false, false)
				intents := &grokVideoBillingIntentRepo{}
				accountRepo := openAIImagesFailoverAccountRepo{accounts: []service.Account{{
					ID: 1, Platform: service.PlatformGrok, Type: service.AccountTypeAPIKey,
					Status: service.StatusActive, Schedulable: true, Concurrency: 50, GroupIDs: []int64{24},
					Credentials: map[string]any{"api_key": "test-key"},
				}}}
				cfg := &config.Config{RunMode: config.RunModeStandard}
				cfg.Default.RateMultiplier = 1
				// Keep the existing fixture's isolated admission dependencies while
				// using standard-mode RecordUsage, including its durable money command.
				h.gatewayService = service.NewOpenAIGatewayService(
					accountRepo, nil, intents, nil, nil, nil, bindings, cfg, nil,
					service.NewConcurrencyService(slots), service.NewBillingService(cfg, nil), nil, nil,
					upstream, nil, nil, nil, nil, nil, nil, nil, nil,
				)
				require.NoError(t, h.gatewayService.StoreGrokVideoPendingBilling(context.Background(), "task", 10, 20, service.GrokVideoPendingBilling{
					Model: "grok-imagine-video", BillingModel: "grok-imagine-video",
					VideoDurationSeconds: 6, VideoResolution: "720p",
				}))
				complete := false
				upstream.call = func(req *http.Request, _ int64) (*http.Response, error) {
					body, contentType := `{"request_id":"task","status":"pending"}`, "application/json"
					if complete {
						body = `{"request_id":"task","status":"done","model":"grok-imagine-video","video":{"url":"https://vidgen.x.ai/test.mp4","duration":6}}`
					}
					if req.URL.Host == "vidgen.x.ai" {
						body, contentType = "fake-video-content", "video/mp4"
					}
					return &http.Response{StatusCode: http.StatusOK,
						Header: http.Header{"Content-Type": []string{contentType}},
						Body:   io.NopCloser(strings.NewReader(body))}, nil
				}
				lookup := func(kind string) {
					t.Helper()
					c, w := grokMediaSlotContext(context.Background(), false)
					key, ok := middleware2.GetAPIKeyFromContext(c)
					require.True(t, ok)
					key.Group.RateMultiplier = 1.5
					key.Group.VideoPrice720P = &unitPrice
					if kind == "content" {
						c.Request.URL.Path = "/v1/videos/task/content"
						h.GrokVideoContent(c)
					} else {
						h.GrokVideoStatus(c)
					}
					require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				}

				lookup("status")
				require.Empty(t, intents.commands, "pending polls do not create generation bills")
				complete = true
				lookup(firstLookup)
				lookup("status")
				lookup("content")

				require.Len(t, intents.commands, 1, "status and content share one generation settlement")
				require.Equal(t, 1, intents.acks, "the money intent reached the durable contract successfully")
				cmd, log := intents.commands[0], intents.logs[0]
				require.Equal(t, "grok-video:task", cmd.RequestID)
				require.Equal(t, int64(20), cmd.APIKeyID)
				require.InDelta(t, unitPrice*6, cmd.TotalCost, 1e-9)
				require.InDelta(t, unitPrice*6*1.5, cmd.BalanceCost, 1e-9)
				require.Equal(t, service.BillingStateSettled, log.BillingState)
				require.Equal(t, 1, log.VideoCount)
				require.NotNil(t, log.VideoDurationSeconds)
				require.Equal(t, 6, *log.VideoDurationSeconds)
				require.NotNil(t, log.VideoResolution)
				require.Equal(t, "720p", *log.VideoResolution)
				require.NotNil(t, log.BillingMode)
				require.Equal(t, string(service.BillingModeVideo), *log.BillingMode)
				require.Equal(t, service.BillingKindNone, service.GrokMediaEndpointVideoStatus.BillingKind())
				require.Equal(t, service.BillingKindNone, service.GrokMediaEndpointVideoContent.BillingKind())
				slots.assertReleased(t)
			})
		}
	}
}
