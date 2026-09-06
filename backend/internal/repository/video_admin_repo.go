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

type videoAdminRepository struct {
	db *sql.DB
}

func NewVideoAdminRepository(db *sql.DB) service.VideoAdminRepository {
	return &videoAdminRepository{db: db}
}

func videoAdminPagination(page, pageSize int) (int, int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize, (page - 1) * pageSize
}

func (r *videoAdminRepository) ListVideoTasksAdmin(ctx context.Context, filter service.VideoAdminTaskFilter) (*service.VideoAdminTaskPage, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("video admin repository is not configured")
	}
	page, pageSize, offset := videoAdminPagination(filter.Page, filter.PageSize)
	args := make([]any, 0, 12)
	where := []string{"TRUE"}
	add := func(format string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(format, len(args)))
	}
	if filter.UserID != nil {
		add("user_id = $%d", *filter.UserID)
	}
	if filter.GroupID != nil {
		add("group_id = $%d", *filter.GroupID)
	}
	if filter.AccountID != nil {
		add("account_id = $%d", *filter.AccountID)
	}
	if value := strings.TrimSpace(filter.Provider); value != "" {
		add("provider = $%d", value)
	}
	if value := strings.TrimSpace(filter.Operation); value != "" {
		add("operation = $%d", value)
	}
	if value := strings.TrimSpace(filter.GenerationState); value != "" {
		add("generation_state = $%d", value)
	}
	if value := strings.TrimSpace(filter.BillingState); value != "" {
		add("billing_state = $%d", value)
	}
	if value := strings.TrimSpace(filter.DeleteState); value != "" {
		add("delete_state = $%d", value)
	}
	if value := strings.TrimSpace(filter.Query); value != "" {
		args = append(args, "%"+value+"%")
		placeholder := fmt.Sprintf("$%d", len(args))
		where = append(where, "(public_id ILIKE "+placeholder+" OR COALESCE(provider_task_id, '') ILIKE "+placeholder+" OR public_model ILIKE "+placeholder+")")
	}
	if filter.CreatedAfter != nil {
		add("created_at >= $%d", *filter.CreatedAfter)
	}
	if filter.CreatedBefore != nil {
		add("created_at <= $%d", *filter.CreatedBefore)
	}

	whereSQL := strings.Join(where, " AND ")
	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM video_tasks WHERE "+whereSQL, args...).Scan(&total); err != nil {
		return nil, err
	}
	args = append(args, pageSize, offset)
	query := fmt.Sprintf(`
		SELECT to_jsonb(vt) FROM video_tasks vt
		WHERE %s
		ORDER BY created_at DESC, id DESC
		LIMIT $%d OFFSET $%d
	`, whereSQL, len(args)-1, len(args))
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	tasks := make([]*service.VideoTask, 0, pageSize)
	for rows.Next() {
		task, err := scanVideoTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &service.VideoAdminTaskPage{Tasks: tasks, Total: total, Page: page, PageSize: pageSize}, nil
}

func (r *videoAdminRepository) GetVideoTaskAdmin(ctx context.Context, publicID string) (*service.VideoTask, error) {
	task, err := scanVideoTask(r.db.QueryRowContext(ctx, `SELECT to_jsonb(vt) FROM video_tasks vt WHERE public_id = $1`, strings.TrimSpace(publicID)))
	return translateVideoTaskNotFound(task, err)
}

func (r *videoAdminRepository) ListVideoResourcesAdmin(ctx context.Context, filter service.VideoAdminResourceFilter) (*service.VideoAdminResourcePage, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("video admin repository is not configured")
	}
	page, pageSize, offset := videoAdminPagination(filter.Page, filter.PageSize)
	args := make([]any, 0, 8)
	where := []string{"TRUE"}
	add := func(format string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(format, len(args)))
	}
	if filter.UserID != nil {
		add("user_id = $%d", *filter.UserID)
	}
	if filter.AccountID != nil {
		add("account_id = $%d", *filter.AccountID)
	}
	if value := strings.TrimSpace(filter.Provider); value != "" {
		add("provider = $%d", value)
	}
	if value := strings.TrimSpace(filter.Status); value != "" {
		add("status = $%d", value)
	}
	if value := strings.TrimSpace(filter.Query); value != "" {
		args = append(args, "%"+value+"%")
		placeholder := fmt.Sprintf("$%d", len(args))
		where = append(where, "(public_id ILIKE "+placeholder+" OR provider_resource_id ILIKE "+placeholder+" OR model ILIKE "+placeholder+")")
	}
	whereSQL := strings.Join(where, " AND ")
	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM video_resources WHERE "+whereSQL, args...).Scan(&total); err != nil {
		return nil, err
	}
	args = append(args, pageSize, offset)
	query := fmt.Sprintf(`
		SELECT to_jsonb(vr) FROM video_resources vr
		WHERE %s
		ORDER BY created_at DESC, id DESC
		LIMIT $%d OFFSET $%d
	`, whereSQL, len(args)-1, len(args))
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	resources := make([]*service.VideoResource, 0, pageSize)
	for rows.Next() {
		resource, err := scanVideoResource(rows)
		if err != nil {
			return nil, err
		}
		resources = append(resources, resource)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &service.VideoAdminResourcePage{Resources: resources, Total: total, Page: page, PageSize: pageSize}, nil
}

func (r *videoAdminRepository) GetVideoResourceAdmin(ctx context.Context, publicID string) (*service.VideoResource, error) {
	resource, err := scanVideoResource(r.db.QueryRowContext(ctx, `SELECT to_jsonb(vr) FROM video_resources vr WHERE public_id = $1`, strings.TrimSpace(publicID)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrVideoResourceNotFound
	}
	return resource, err
}

type videoAdminEventRow struct {
	ID                  int64          `json:"id"`
	TaskID              *int64         `json:"task_id"`
	EventType           string         `json:"event_type"`
	Provider            *string        `json:"provider"`
	AccountID           *int64         `json:"account_id"`
	ProviderTaskID      *string        `json:"provider_task_id"`
	ProviderEventID     *string        `json:"provider_event_id"`
	FromGenerationState *string        `json:"from_generation_state"`
	ToGenerationState   *string        `json:"to_generation_state"`
	FromBillingState    *string        `json:"from_billing_state"`
	ToBillingState      *string        `json:"to_billing_state"`
	Payload             map[string]any `json:"payload"`
	EventHash           *string        `json:"event_hash"`
	CreatedAt           time.Time      `json:"created_at"`
}

func scanVideoAdminEvent(scanner videoJSONScanner) (*service.VideoTaskEvent, error) {
	var raw []byte
	if err := scanner.Scan(&raw); err != nil {
		return nil, err
	}
	var row videoAdminEventRow
	if err := json.Unmarshal(raw, &row); err != nil {
		return nil, fmt.Errorf("decode video task event row: %w", err)
	}
	return &service.VideoTaskEvent{
		ID: row.ID, TaskID: row.TaskID, EventType: row.EventType,
		Provider: valueOrEmptyString(row.Provider), AccountID: row.AccountID,
		ProviderTaskID: valueOrEmptyString(row.ProviderTaskID), ProviderEventID: valueOrEmptyString(row.ProviderEventID),
		FromGenerationState: valueOrEmptyString(row.FromGenerationState), ToGenerationState: valueOrEmptyString(row.ToGenerationState),
		FromBillingState: valueOrEmptyString(row.FromBillingState), ToBillingState: valueOrEmptyString(row.ToBillingState),
		Payload: row.Payload, EventHash: valueOrEmptyString(row.EventHash), CreatedAt: row.CreatedAt,
	}, nil
}

func (r *videoAdminRepository) ListVideoTaskEventsAdmin(ctx context.Context, publicID string, page, pageSize int) (*service.VideoAdminEventPage, error) {
	var taskID int64
	if err := r.db.QueryRowContext(ctx, `SELECT id FROM video_tasks WHERE public_id = $1`, strings.TrimSpace(publicID)).Scan(&taskID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrVideoTaskNotFound
		}
		return nil, err
	}
	return r.listVideoEvents(ctx, "task_id = $1", []any{taskID}, page, pageSize)
}

func (r *videoAdminRepository) ListUnmatchedVideoEventsAdmin(ctx context.Context, page, pageSize int) (*service.VideoAdminEventPage, error) {
	return r.listVideoEvents(ctx, "task_id IS NULL", nil, page, pageSize)
}

func (r *videoAdminRepository) listVideoEvents(ctx context.Context, where string, args []any, page, pageSize int) (*service.VideoAdminEventPage, error) {
	page, pageSize, offset := videoAdminPagination(page, pageSize)
	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM video_task_events WHERE "+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	args = append(args, pageSize, offset)
	query := fmt.Sprintf(`
		SELECT to_jsonb(vte) FROM video_task_events vte
		WHERE %s ORDER BY created_at DESC, id DESC LIMIT $%d OFFSET $%d
	`, where, len(args)-1, len(args))
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	events := make([]*service.VideoTaskEvent, 0, pageSize)
	for rows.Next() {
		event, err := scanVideoAdminEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &service.VideoAdminEventPage{Events: events, Total: total, Page: page, PageSize: pageSize}, nil
}

func (r *videoAdminRepository) ListVideoCallbacksAdmin(ctx context.Context, filter service.VideoAdminCallbackFilter) (*service.VideoAdminCallbackPage, error) {
	page, pageSize, offset := videoAdminPagination(filter.Page, filter.PageSize)
	args := make([]any, 0, 4)
	where := []string{"TRUE"}
	if value := strings.TrimSpace(filter.TaskPublicID); value != "" {
		args = append(args, value)
		where = append(where, fmt.Sprintf("vt.public_id = $%d", len(args)))
	}
	if value := strings.TrimSpace(filter.Status); value != "" {
		args = append(args, value)
		where = append(where, fmt.Sprintf("vcd.status = $%d", len(args)))
	}
	whereSQL := strings.Join(where, " AND ")
	var total int
	countQuery := "SELECT COUNT(*) FROM video_callback_deliveries vcd JOIN video_tasks vt ON vt.id = vcd.task_id WHERE " + whereSQL
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, err
	}
	args = append(args, pageSize, offset)
	query := fmt.Sprintf(`
		SELECT to_jsonb(vcd) FROM video_callback_deliveries vcd
		JOIN video_tasks vt ON vt.id = vcd.task_id
		WHERE %s ORDER BY vcd.created_at DESC, vcd.id DESC LIMIT $%d OFFSET $%d
	`, whereSQL, len(args)-1, len(args))
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	callbacks := make([]*service.VideoCallbackDelivery, 0, pageSize)
	for rows.Next() {
		delivery, err := scanVideoCallback(rows)
		if err != nil {
			return nil, err
		}
		callbacks = append(callbacks, delivery)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &service.VideoAdminCallbackPage{Callbacks: callbacks, Total: total, Page: page, PageSize: pageSize}, nil
}

func (r *videoAdminRepository) RetryVideoCallbackAdmin(ctx context.Context, id int64) (_ *service.VideoCallbackDelivery, err error) {
	if id <= 0 {
		return nil, service.ErrVideoInvalidRequest
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	delivery, err := scanVideoCallback(tx.QueryRowContext(ctx, `SELECT to_jsonb(vcd) FROM video_callback_deliveries vcd WHERE id = $1 FOR UPDATE`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrVideoTaskNotFound
	}
	if err != nil {
		return nil, err
	}
	if delivery.Status == "delivered" || delivery.Status == "delivering" || !time.Now().UTC().Before(delivery.ExpiresAt) {
		return nil, service.ErrVideoInvalidTransition
	}
	if _, err = tx.ExecContext(ctx, `
		UPDATE video_callback_deliveries
		SET status = 'pending', next_attempt_at = NOW(), lease_owner = NULL,
			lease_expires_at = NULL, quarantined_at = NULL, updated_at = NOW()
		WHERE id = $1
	`, id); err != nil {
		return nil, err
	}
	delivery, err = scanVideoCallback(tx.QueryRowContext(ctx, `SELECT to_jsonb(vcd) FROM video_callback_deliveries vcd WHERE id = $1`, id))
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return delivery, nil
}

func (r *videoAdminRepository) GetVideoAdminOverview(ctx context.Context) (*service.VideoAdminOverview, error) {
	overview := &service.VideoAdminOverview{
		TasksByGeneration: make(map[string]int64), TasksByBilling: make(map[string]int64),
		TasksByDelete: make(map[string]int64), CallbacksByStatus: make(map[string]int64),
	}
	if err := r.loadVideoAdminCounts(ctx, `SELECT generation_state, COUNT(*) FROM video_tasks GROUP BY generation_state`, overview.TasksByGeneration); err != nil {
		return nil, err
	}
	if err := r.loadVideoAdminCounts(ctx, `SELECT billing_state, COUNT(*) FROM video_tasks GROUP BY billing_state`, overview.TasksByBilling); err != nil {
		return nil, err
	}
	if err := r.loadVideoAdminCounts(ctx, `SELECT delete_state, COUNT(*) FROM video_tasks GROUP BY delete_state`, overview.TasksByDelete); err != nil {
		return nil, err
	}
	if err := r.loadVideoAdminCounts(ctx, `SELECT status, COUNT(*) FROM video_callback_deliveries GROUP BY status`, overview.CallbacksByStatus); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT vt.provider, vt.operation, vt.generation_state, COUNT(*),
			MIN(COALESCE((
				SELECT MAX(vte.created_at)
				FROM video_task_events vte
				WHERE vte.task_id = vt.id AND vte.to_generation_state = vt.generation_state
			), vt.created_at))
		FROM video_tasks vt
		GROUP BY vt.provider, vt.operation, vt.generation_state
		ORDER BY vt.provider, vt.operation, vt.generation_state
	`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var state service.VideoTaskStateSnapshot
		var oldest time.Time
		if err := rows.Scan(&state.Provider, &state.Operation, &state.State, &state.Count, &oldest); err != nil {
			_ = rows.Close()
			return nil, err
		}
		state.OldestEnteredAt = &oldest
		overview.TaskStates = append(overview.TaskStates, state)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(hold_amount), 0)
		FROM video_tasks WHERE generation_state = 'submission_unknown'
	`).Scan(&overview.SubmissionUnknown, &overview.UnknownHoldAmount); err != nil {
		return nil, err
	}
	if err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(hold_amount), 0)
		FROM video_tasks
		WHERE billing_state IN ('held','capture_pending','release_pending','manual_review')
	`).Scan(&overview.HeldAmount); err != nil {
		return nil, err
	}
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM video_task_events WHERE task_id IS NULL`).Scan(&overview.UnmatchedWebhooks); err != nil {
		return nil, err
	}
	if err := scanNullableTime(r.db.QueryRowContext(ctx, `
		SELECT MIN(next_action_at) FROM video_tasks
		WHERE next_action_at IS NOT NULL AND (
			generation_state IN ('submission_unknown','queued','in_progress')
			OR billing_state IN ('capture_pending','release_pending')
			OR delete_state IN ('requested','deleting','delete_failed')
		)
	`), &overview.OldestTaskPendingAt); err != nil {
		return nil, err
	}
	if err := scanNullableTime(r.db.QueryRowContext(ctx, `
		SELECT MIN(COALESCE((
			SELECT MAX(vte.created_at) FROM video_task_events vte
			WHERE vte.task_id = vt.id AND vte.to_billing_state = vt.billing_state
		), vt.updated_at))
		FROM video_tasks vt WHERE billing_state IN ('capture_pending','release_pending')
	`), &overview.OldestBillingAt); err != nil {
		return nil, err
	}
	if err := scanNullableTime(r.db.QueryRowContext(ctx, `
		SELECT MIN(COALESCE((
			SELECT MAX(vte.created_at) FROM video_task_events vte
			WHERE vte.task_id = vt.id AND vte.to_billing_state = vt.billing_state
		), vt.updated_at))
		FROM video_tasks vt WHERE billing_state = 'manual_review'
	`), &overview.OldestManualReviewAt); err != nil {
		return nil, err
	}
	if err := scanNullableTime(r.db.QueryRowContext(ctx, `
		SELECT MIN(next_attempt_at) FROM video_callback_deliveries WHERE status IN ('pending','failed','delivering')
	`), &overview.OldestCallbackAt); err != nil {
		return nil, err
	}
	return overview, nil
}

func (r *videoAdminRepository) loadVideoAdminCounts(ctx context.Context, query string, target map[string]int64) error {
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var key string
		var count int64
		if err := rows.Scan(&key, &count); err != nil {
			return err
		}
		target[key] = count
	}
	return rows.Err()
}

func scanNullableTime(row *sql.Row, target **time.Time) error {
	var value sql.NullTime
	if err := row.Scan(&value); err != nil {
		return err
	}
	if value.Valid {
		v := value.Time
		*target = &v
	}
	return nil
}
