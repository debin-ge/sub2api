//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestVideoExecutionSpecPersistsAndQuarantinesConflictingObservation(t *testing.T) {
	for _, failure := range []string{"provider_model", "snapshot_hash"} {
		t.Run(failure, func(t *testing.T) {
			ctx := context.Background()
			repo, resources, _, user, key, account := newVideoRepositoryFixture(t, 100)
			params := videoCreateParams(user, key, account, service.NewVideoTaskID(), failure, "spec-body", 4)
			spec := service.ResolvedVideoExecutionSpec{Version: 2, Provider: service.VideoProviderOpenAI, AccountID: account.ID,
				Operation: service.VideoOperationGenerate, Model: service.OpenAIVideoModelSora2, Size: "1280x720", Seconds: 8, DurationSemantics: "output"}
			fingerprint, err := service.HashVideoRequest(spec)
			require.NoError(t, err)
			params.RequestAttributes["execution_spec"], params.RequestAttributes["execution_spec_hash"], params.RequestAttributes["execution_spec_version"] = spec, fingerprint, 2
			if failure == "snapshot_hash" {
				params.RequestAttributes["execution_spec_hash"] = "invalid-preexisting-fingerprint"
			}
			params.PriceSnapshot["unit_price"], params.PriceSnapshot["customer_multiplier"] = 0.5, 1
			task, _, err := repo.CreateHeldVideoTask(ctx, params)
			require.NoError(t, err)
			task, err = repo.TransitionVideoTask(videoRepositoryWriteContext(t, repo, ctx, task.PublicID), task.PublicID,
				service.VideoTaskTransition{GenerationState: service.VideoGenerationSubmitting})
			require.NoError(t, err)
			task, err = repo.SaveVideoProviderAccepted(videoRepositoryWriteContext(t, repo, ctx, task.PublicID), task.PublicID,
				service.VideoProviderAcceptance{ProviderTaskID: "video_spec_" + failure, GenerationState: service.VideoGenerationQueued})
			require.NoError(t, err)
			svc := service.NewVideoTaskService(repo, resources, nil, nil, nil, nil, nil, nil, nil, nil, repo.billing, nil, nil, &config.Config{})
			observed := &service.ProviderVideoTask{ProviderTaskID: *task.ProviderTaskID, Status: service.VideoGenerationInProgress,
				Metadata: map[string]any{"model": service.OpenAIVideoModelSora2, "size": "1280x720", "seconds": "8"}}
			task, err = svc.ReconcileProviderObservation(ctx, task, observed, "provider_polled")
			require.NoError(t, err)
			if failure == "provider_model" {
				require.NotContains(t, task.ResponseMetadata, "execution_spec_conflict")
				observed.Metadata["model"] = service.OpenAIVideoModelSora2Pro
			}
			task, err = svc.ReconcileProviderObservation(ctx, task, observed, "provider_polled")
			require.NoError(t, err)
			require.Equal(t, service.VideoBillingHeld, task.BillingState)
			require.NotNil(t, task.NextActionAt)
			require.Equal(t, float64(1), task.ResponseMetadata["execution_spec_conflict"])
			task, err = repo.GetVideoTaskByPublicID(ctx, task.PublicID)
			require.NoError(t, err)
			observed.Status, observed.Metadata["model"] = service.VideoGenerationCompleted, service.OpenAIVideoModelSora2
			task, err = svc.ReconcileProviderObservation(ctx, task, observed, "provider_polled")
			require.NoError(t, err)
			require.Equal(t, service.VideoBillingManualReview, task.BillingState)
			require.NotNil(t, task.LastErrorCode)
			require.Equal(t, "execution_spec_conflict", *task.LastErrorCode)
			assertVideoBudgetTotals(t, user.ID, 1, 96, 4)
			var intents int
			require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_billing_outbox WHERE api_key_id = $1`, key.ID).Scan(&intents))
			require.Zero(t, intents)
		})
	}
}
