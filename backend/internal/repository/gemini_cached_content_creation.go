package repository

import (
	"context"
	"math"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *geminiCachedContentRepository) BeginCreation(ctx context.Context, intent *service.GeminiCachedContentCreationIntent) error {
	result, err := r.sql.ExecContext(ctx, `INSERT INTO gemini_cache_creation_intents
 (public_id, user_id, api_key_id, account_id, account_identity_version, upstream_model, ttl_seconds)
 VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT DO NOTHING`,
		intent.PublicID, intent.UserID, intent.APIKeyID, intent.AccountID,
		intent.AccountIdentityVersion, intent.UpstreamModel, int64(math.Ceil(intent.TTL.Seconds())))
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return service.ErrGeminiCachedContentCreationPending
	}
	return nil
}

func (r *geminiCachedContentRepository) ResolveCreation(ctx context.Context, publicID, resolution string) error {
	_, err := r.sql.ExecContext(ctx, `UPDATE gemini_cache_creation_intents
 SET resolved_at = CURRENT_TIMESTAMP, resolution = $2 WHERE public_id = $1 AND resolved_at IS NULL`, publicID, resolution)
	return err
}

var _ service.GeminiCachedContentCreationRepository = (*geminiCachedContentRepository)(nil)
