package repository

import (
	"context"
	"database/sql"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Called inside the fenced task transition, so a lost lease or failed state
// write cannot leave an audit record claiming that the observation was applied.
func insertBlockedVideoProviderActionsTx(ctx context.Context, tx *sql.Tx, taskID int64, actions []service.VideoProviderBlockedAction) error {
	for _, action := range actions {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO video_provider_blocked_actions (task_id, action_key, action_type, reason, quoted_credits)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (task_id, action_key) DO UPDATE
			SET last_observed_at = NOW(),
				quote_conflict = video_provider_blocked_actions.quote_conflict OR
					(video_provider_blocked_actions.quoted_credits IS DISTINCT FROM EXCLUDED.quoted_credits)
		`, taskID, action.ActionKey, action.ActionType, action.Reason, action.QuotedCredits)
		if err != nil {
			return err
		}
	}
	return nil
}
