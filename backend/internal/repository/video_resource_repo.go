package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type videoResourceRepository struct {
	db *sql.DB
}

func NewVideoResourceRepository(db *sql.DB) service.VideoResourceRepository {
	return &videoResourceRepository{db: db}
}

type videoResourceRow struct {
	ID                      int64          `json:"id"`
	PublicID                string         `json:"public_id"`
	ResourceType            string         `json:"resource_type"`
	UserID                  int64          `json:"user_id"`
	APIKeyID                *int64         `json:"api_key_id"`
	GroupID                 *int64         `json:"group_id"`
	Provider                string         `json:"provider"`
	ChannelID               *int64         `json:"channel_id"`
	AccountID               int64          `json:"account_id"`
	SourceTaskID            *int64         `json:"source_task_id"`
	ProviderResourceID      string         `json:"provider_resource_id"`
	Model                   string         `json:"model"`
	Status                  string         `json:"status"`
	Metadata                map[string]any `json:"metadata"`
	ProviderAccessKind      *string        `json:"provider_access_kind"`
	ProviderAccessScope     *string        `json:"provider_access_scope"`
	ProviderAccessEnc       *string        `json:"provider_access_enc"`
	ProviderAccessExpiresAt *time.Time     `json:"provider_access_expires_at"`
	Version                 int64          `json:"version"`
	CreatedAt               time.Time      `json:"created_at"`
	UpdatedAt               time.Time      `json:"updated_at"`
	ExpiresAt               *time.Time     `json:"expires_at"`
	DeletedAt               *time.Time     `json:"deleted_at"`
}

func (row videoResourceRow) resource() *service.VideoResource {
	return &service.VideoResource{
		ID: row.ID, PublicID: row.PublicID, ResourceType: row.ResourceType,
		UserID: row.UserID, APIKeyID: row.APIKeyID, GroupID: row.GroupID,
		Provider: row.Provider, ChannelID: row.ChannelID, AccountID: row.AccountID,
		SourceTaskID: row.SourceTaskID, ProviderResourceID: row.ProviderResourceID,
		Model: row.Model, Status: row.Status, Metadata: row.Metadata,
		ProviderAccessKind: row.ProviderAccessKind, ProviderAccessScope: row.ProviderAccessScope,
		ProviderAccessEnc: row.ProviderAccessEnc, ProviderAccessExpires: row.ProviderAccessExpiresAt,
		Version: row.Version, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		ExpiresAt: row.ExpiresAt, DeletedAt: row.DeletedAt,
	}
}

func scanVideoResource(scanner videoJSONScanner) (*service.VideoResource, error) {
	var raw []byte
	if err := scanner.Scan(&raw); err != nil {
		return nil, err
	}
	var row videoResourceRow
	if err := json.Unmarshal(raw, &row); err != nil {
		return nil, fmt.Errorf("decode video resource row: %w", err)
	}
	return row.resource(), nil
}

func (r *videoResourceRepository) CreateVideoResource(ctx context.Context, params service.VideoCreateResourceParams) (*service.VideoResource, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("video resource repository is not configured")
	}
	return createVideoResourceQuery(ctx, r.db, params)
}

type videoResourceQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func createVideoResourceQuery(ctx context.Context, database videoResourceQuerier, params service.VideoCreateResourceParams) (*service.VideoResource, error) {
	if params.PublicID == "" {
		params.PublicID = service.NewVideoResourceID()
	}
	if params.ResourceType == "" {
		params.ResourceType = service.VideoResourceTypeCharacter
	}
	if !service.IsValidVideoResourceID(params.PublicID) || params.ResourceType != service.VideoResourceTypeCharacter ||
		params.Owner.UserID <= 0 || params.AccountID <= 0 || strings.TrimSpace(params.ProviderResourceID) == "" {
		return nil, service.ErrVideoInvalidRequest
	}
	if params.Provider == "" {
		params.Provider = service.VideoProviderOpenAI
	}
	if params.Status == "" {
		params.Status = "ready"
	}
	metadata, err := videoJSON(params.Metadata, map[string]any{})
	if err != nil {
		return nil, err
	}
	var id int64
	err = database.QueryRowContext(ctx, `
		INSERT INTO video_resources (
			public_id, resource_type, user_id, api_key_id, group_id, provider,
			channel_id, account_id, source_task_id, provider_resource_id,
			model, status, metadata, expires_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb, $14)
		RETURNING id
	`, params.PublicID, params.ResourceType, params.Owner.UserID, params.Owner.APIKeyID,
		params.Owner.GroupID, params.Provider, params.ChannelID, params.AccountID,
		params.SourceTaskID, params.ProviderResourceID, params.Model, params.Status,
		metadata, params.ExpiresAt).Scan(&id)
	if err != nil {
		return nil, err
	}
	return scanVideoResource(database.QueryRowContext(ctx, `SELECT to_jsonb(vr) FROM video_resources vr WHERE id = $1`, id))
}

func (r *videoResourceRepository) GetVideoResourceForOwner(ctx context.Context, userID int64, publicID string) (*service.VideoResource, error) {
	resource, err := scanVideoResource(r.db.QueryRowContext(ctx, `
		SELECT to_jsonb(vr) FROM video_resources vr
		WHERE user_id = $1 AND public_id = $2 AND deleted_at IS NULL
	`, userID, strings.TrimSpace(publicID)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrVideoResourceNotFound
	}
	return resource, err
}

func (r *videoResourceRepository) GetVideoResourceBySourceTaskForOwner(ctx context.Context, userID int64, sourceTaskID int64) (*service.VideoResource, error) {
	resource, err := scanVideoResource(r.db.QueryRowContext(ctx, `
		SELECT to_jsonb(vr) FROM video_resources vr
		WHERE user_id = $1 AND source_task_id = $2 AND deleted_at IS NULL
	`, userID, sourceTaskID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrVideoResourceNotFound
	}
	return resource, err
}

func (r *videoResourceRepository) GetVideoResourceForOwnerIncludingDeleted(ctx context.Context, userID int64, publicID string) (*service.VideoResource, error) {
	resource, err := scanVideoResource(r.db.QueryRowContext(ctx, `
		SELECT to_jsonb(vr) FROM video_resources vr WHERE user_id=$1 AND public_id=$2
	`, userID, strings.TrimSpace(publicID)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrVideoResourceNotFound
	}
	return resource, err
}

func (r *videoResourceRepository) GetVideoResourceByProviderID(ctx context.Context, provider string, accountID int64, providerResourceID string) (*service.VideoResource, error) {
	resource, err := scanVideoResource(r.db.QueryRowContext(ctx, `
		SELECT to_jsonb(vr) FROM video_resources vr
		WHERE provider = $1 AND account_id = $2 AND provider_resource_id = $3
	`, strings.TrimSpace(provider), accountID, strings.TrimSpace(providerResourceID)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrVideoResourceNotFound
	}
	return resource, err
}

func (r *videoResourceRepository) MarkVideoResourceDeleted(ctx context.Context, userID int64, publicID string) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE video_resources
		SET status = 'deleted', deleted_at = COALESCE(deleted_at, NOW()),
			version = version + 1, updated_at = NOW()
		WHERE user_id = $1 AND public_id = $2 AND deleted_at IS NULL
	`, userID, strings.TrimSpace(publicID))
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrVideoResourceNotFound
	}
	return nil
}
