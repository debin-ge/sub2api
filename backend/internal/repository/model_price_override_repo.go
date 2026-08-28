package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/modelpriceoverride"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type modelPriceOverrideRepository struct {
	client *ent.Client
}

func NewModelPriceOverrideRepository(client *ent.Client) service.ModelPriceOverrideStore {
	return &modelPriceOverrideRepository{client: client}
}

func (r *modelPriceOverrideRepository) List(ctx context.Context) ([]service.ModelPriceOverride, error) {
	rows, err := clientFromContext(ctx, r.client).ModelPriceOverride.Query().
		Order(ent.Asc(modelpriceoverride.FieldPlatform), ent.Asc(modelpriceoverride.FieldModelName)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]service.ModelPriceOverride, 0, len(rows))
	for _, row := range rows {
		converted, err := modelPriceOverrideFromEnt(row)
		if err != nil {
			return nil, err
		}
		result = append(result, *converted)
	}
	return result, nil
}

func (r *modelPriceOverrideRepository) Upsert(ctx context.Context, row *service.ModelPriceOverride) (*service.ModelPriceOverride, error) {
	payload, err := modelPriceOverridePayloadMap(&row.Payload)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	id, err := clientFromContext(ctx, r.client).ModelPriceOverride.Create().
		SetPlatform(row.Platform).
		SetModelName(row.ModelName).
		SetCurrency(modelpriceoverride.Currency(row.Currency)).
		SetPayload(payload).
		SetEnabled(row.Enabled).
		SetNillableNote(row.Note).
		SetNillableUpdatedBy(row.UpdatedBy).
		SetUpdatedAt(now).
		OnConflictColumns(modelpriceoverride.FieldPlatform, modelpriceoverride.FieldModelName).
		UpdateNewValues().
		ID(ctx)
	if err != nil {
		return nil, err
	}
	saved, err := clientFromContext(ctx, r.client).ModelPriceOverride.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return modelPriceOverrideFromEnt(saved)
}

func (r *modelPriceOverrideRepository) Delete(ctx context.Context, platform, model string) error {
	_, err := clientFromContext(ctx, r.client).ModelPriceOverride.Delete().
		Where(
			modelpriceoverride.PlatformEQ(platform),
			modelpriceoverride.ModelNameEQ(model),
		).
		Exec(ctx)
	return err
}

func modelPriceOverridePayloadMap(payload *service.ModelPriceOverridePayload) (map[string]any, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	result := make(map[string]any)
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func modelPriceOverrideFromEnt(row *ent.ModelPriceOverride) (*service.ModelPriceOverride, error) {
	raw, err := json.Marshal(row.Payload)
	if err != nil {
		return nil, err
	}
	payload, err := service.DecodeModelPriceOverridePayload(raw)
	if err != nil {
		return nil, err
	}
	return &service.ModelPriceOverride{
		ID:        row.ID,
		Platform:  row.Platform,
		ModelName: row.ModelName,
		Currency:  string(row.Currency),
		Payload:   payload,
		Enabled:   row.Enabled,
		Note:      row.Note,
		UpdatedBy: row.UpdatedBy,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}, nil
}
