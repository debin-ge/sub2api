package service

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func (w *VideoCallbackWorker) materializeCallbacks(ctx context.Context, limit int) error {
	repository, supported := w.repository.(VideoCallbackMaterializationRepository)
	if !supported {
		return nil
	}
	tasks, err := repository.ListVideoCallbackIntents(ctx, limit)
	if err != nil {
		return err
	}
	var failures []error
	for _, task := range tasks {
		policy := config.VideoDisclosureNone
		if w.tasks != nil {
			policy, _ = w.tasks.videoDisclosurePolicy(ctx, task)
		}
		ceiling, _ := task.RequestAttributes["callback_disclosure_policy"].(string)
		policy = effectiveVideoDisclosurePolicy(policy, ceiling, config.VideoDisclosureIdentity)
		delivery, err := buildDurableVideoCallback(task, w.cfg, w.now().UTC(), policy)
		if err == nil {
			err = repository.MaterializeVideoCallback(ctx, delivery)
		}
		if err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func buildDurableVideoCallback(task *VideoTask, cfg *config.Config, now time.Time, policy string) (*VideoCallbackDelivery, error) {
	if task == nil || task.CallbackURLEnc == nil || cfg == nil {
		return nil, ErrVideoInvalidRequest
	}
	builderConfig := &config.Config{Gateway: config.GatewayConfig{Video: config.GatewayVideoConfig{Callback: cfg.Gateway.Video.Callback}}}
	builderConfig.Gateway.Video.Callback.Enabled = true
	delivery, needed, err := BuildVideoCallbackDelivery(task, builderConfig, now, policy)
	if err != nil {
		return nil, err
	}
	if !needed {
		fingerprint, err := HashVideoRequest(map[string]any{"task_id": task.ID, "event_type": "video.callback_invalid_state"})
		if err != nil {
			return nil, err
		}
		delivery = &VideoCallbackDelivery{
			TaskID: task.ID, EventID: "video_evt_" + fingerprint[:32], EventType: "video.callback_invalid_state", EventFingerprint: fingerprint,
			TargetURLEnc: *task.CallbackURLEnc, Payload: map[string]any{"id": task.PublicID},
			CreatedAt: task.CreatedAt, NextAttemptAt: now, ExpiresAt: now,
		}
	}
	version, versionKnown := numericMapValue(task.RequestAttributes, "callback_contract_version")
	hours, hoursKnown := numericMapValue(task.RequestAttributes, "callback_retry_hours")
	reason := ""
	if !needed {
		reason = "callback terminal state is inconsistent"
	} else if !versionKnown || version != 1 || !hoursKnown || hours <= 0 || hours > 8760 || hours != math.Trunc(hours) || task.SettledAt == nil {
		reason = "callback retry contract is unavailable; manual review required"
		delivery.ExpiresAt = now.Add(-time.Second)
		if task.SettledAt == nil {
			delivery.CreatedAt = task.CreatedAt
			delivery.Payload["created_at"] = task.CreatedAt.Unix()
		}
	} else if !delivery.ExpiresAt.After(now) {
		reason = "callback retry window expired before materialization"
	}
	if reason != "" {
		delivery.Status = "quarantined"
		delivery.LastError = &reason
	}
	return delivery, nil
}
